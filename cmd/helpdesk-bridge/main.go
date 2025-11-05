// Package main implements the odoo-helpdesk-bridge service that bridges IMAP email, Odoo Helpdesk, and Slack.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/anaryk/odoo-helpdesk-bridge/internal/config"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/imap"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/mailer"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/odoo"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/sla"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/slack"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/state"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/templ"
)

const (
	// Preview text length for debug logging
	previewLength = 100

	// Minimum length for text truncation
	minTruncateLength = 3

	// Default operator name for unassigned tickets
	defaultOperatorName = "Nepřiřazeno"
)

func main() {
	// Setup basic zerolog
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	cfgPath := "config.yaml"
	if len(os.Args) > 1 {
		cfgPath = os.Args[1]
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatal().Err(err).Msg("load config")
	}

	// Configure log level based on debug flag
	if cfg.App.Debug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
		log.Debug().Msg("debug logging enabled")
	} else {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// state (bbolt)
	st, err := state.New(cfg.App.StatePath)
	if err != nil {
		log.Error().Err(err).Msg("failed to initialize state")
		return
	}
	defer func() {
		if err := st.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close state store")
		}
	}()

	// templates
	tm, err := templ.New(cfg.TemplatesDirOrDefault())
	if err != nil {
		log.Error().Err(err).Msg("failed to initialize templates")
		return
	}

	// mailer
	m := mailer.NewSMTP(mailer.SMTPConfig{
		Host:      cfg.SMTP.Host,
		Port:      cfg.SMTP.Port,
		Username:  cfg.SMTP.Username,
		Password:  cfg.SMTP.Password,
		FromName:  cfg.SMTP.FromName,
		FromEmail: cfg.SMTP.FromEmail,
		Timeout:   time.Duration(cfg.SMTP.TimeoutSeconds) * time.Second,
	})

	// slack
	sl := slack.NewWithConfig(slack.Config{
		WebhookURL: cfg.Slack.WebhookURL,
		BotToken:   cfg.Slack.BotToken,
		ChannelID:  cfg.Slack.ChannelID,
	})

	// odoo client
	oc, err := odoo.NewClient(ctx, odoo.Config{
		URL:     cfg.Odoo.URL,
		DB:      cfg.Odoo.DB,
		User:    cfg.Odoo.Username,
		Pass:    cfg.Odoo.Password,
		Timeout: time.Duration(cfg.Odoo.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("odoo") //nolint:gocritic // Log.Fatal is intentionally used for startup failure
	}

	// imap
	im, err := imap.New(imap.Config{
		Host:             cfg.IMAP.Host,
		Port:             cfg.IMAP.Port,
		Username:         cfg.IMAP.Username,
		Password:         cfg.IMAP.Password,
		Folder:           cfg.IMAP.Folder,
		SearchTo:         cfg.IMAP.SearchTo,
		ProcessedKeyword: cfg.IMAP.CustomProcessedFlag,
	})
	if err != nil {
		log.Fatal().Err(err).Msg("imap")
	}
	defer func() {
		if err := im.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close IMAP connection")
		}
	}()

	// sla handler
	slaHandler := sla.New(cfg, oc, sl, st)

	// prvotní běh
	if err := processIncoming(ctx, cfg, im, oc, sl, st, tm, m, slaHandler); err != nil {
		log.Error().Err(err).Msg("initial incoming")
	}
	if err := processOdooEvents(ctx, cfg, oc, st, tm, m, sl); err != nil {
		log.Error().Err(err).Msg("odoo events")
	}
	if err := slaHandler.CheckSLAViolations(ctx); err != nil {
		log.Error().Err(err).Msg("initial SLA check")
	}

	// Create gocron scheduler
	scheduler, err := gocron.NewScheduler()
	if err != nil {
		log.Fatal().Err(err).Msg("scheduler")
	}

	// Schedule periodic jobs
	_, err = scheduler.NewJob(
		gocron.DurationJob(time.Duration(cfg.App.PollSeconds)*time.Second),
		gocron.NewTask(func() {
			if err := processIncoming(ctx, cfg, im, oc, sl, st, tm, m, slaHandler); err != nil {
				log.Error().Err(err).Msg("incoming")
			}
			if err := processOdooEvents(ctx, cfg, oc, st, tm, m, sl); err != nil {
				log.Error().Err(err).Msg("odoo")
			}
			if err := slaHandler.CheckSLAViolations(ctx); err != nil {
				log.Error().Err(err).Msg("SLA check")
			}
		}),
	)
	if err != nil {
		log.Fatal().Err(err).Msg("schedule job")
	}

	// Start scheduler
	scheduler.Start()
	defer func() {
		if err := scheduler.Shutdown(); err != nil {
			log.Error().Err(err).Msg("scheduler shutdown")
		}
	}()

	log.Info().Int("poll_seconds", cfg.App.PollSeconds).Msg("helpdesk bridge started")

	// Graceful shutdown
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig

	log.Info().Msg("shutting down...")
	cancel()
}

//nolint:gocyclo // This is the main processing function and complexity is acceptable
func processIncoming(
	ctx context.Context,
	cfg *config.Config,
	im *imap.Client,
	oc *odoo.Client,
	sl *slack.Client,
	st *state.Store,
	tm *templ.Engine,
	m *mailer.SMTPClient,
	slaHandler *sla.Handler,
) error {
	msgs, err := im.FetchUnseen(ctx)
	if err != nil {
		return err
	}

	log.Debug().Int("count", len(msgs)).Msg("fetched unseen messages")

	for _, em := range msgs {
		log.Debug().Str("id", em.ID).Str("from", em.FromEmail).Str("subject", em.Subject).Msg("processing email")

		if ok, _ := st.IsProcessedEmail(em.ID); ok {
			log.Debug().Str("id", em.ID).Msg("email already processed, skipping")
			continue
		}

		// Skip excluded emails (Gmail system emails, etc.)
		if isExcludedEmail(em.FromEmail, cfg.App.ExcludedEmails) {
			log.Info().Str("email", em.FromEmail).Msg("skipping excluded email")
			_ = st.MarkProcessedEmail(em.ID)
			_ = im.MarkSeen(ctx, em.UID)
			continue
		}

		taskID, hasTicket := imap.ExtractTicketID(em.Subject, cfg.App.TicketPrefix)
		if hasTicket {
			log.Debug().Int("task_id", taskID).Str("subject", em.Subject).Msg("found existing ticket ID in subject")

			// odpověď zákazníka -> zkontrolovat zda je task uzavřený a znovu ho otevřít
			taskIDInt64 := int64(taskID)

			// Check if task is closed and reopen if necessary
			log.Debug().Int("task_id", taskID).Msg("checking if task needs to be reopened")
			wasReopened, err := oc.ReopenTask(ctx, taskIDInt64, cfg.Odoo.Stages.New)
			//nolint:gocritic // if-else chain is clearer than switch for this error handling pattern
			if err != nil {
				log.Error().Err(err).Int("task_id", taskID).Msg("failed to reopen task")
				// Continue processing even if reopen fails
			} else if wasReopened {
				log.Info().Int("task_id", taskID).Msg("task was reopened from closed state")

				// Automatically assign operator to reopened task
				var assignedOperator string
				if len(cfg.App.Operators) > 0 {
					var err error
					assignedOperator, err = assignTaskToOperator(ctx, oc, taskIDInt64, cfg)
					if err != nil {
						log.Error().Err(err).Int64("task_id", taskIDInt64).Msg("auto assignment to reopened task failed")
					} else {
						log.Info().Str("operator", assignedOperator).Int64("task_id", taskIDInt64).Msg("reopened task auto-assigned")
					}
				}

				// Update Slack message and notify in thread about task reopening
				if slackInfo, err := st.GetSlackMessage(taskIDInt64); err == nil && slackInfo != nil {
					slackMsg := &slack.Message{
						Timestamp: slackInfo.Timestamp,
						Channel:   slackInfo.Channel,
					}

					// Get task info for URL
					if task, err := oc.GetTask(ctx, taskIDInt64); err == nil {
						operatorName := assignedOperator
						if operatorName == "" {
							operatorName = defaultOperatorName
						}

						// Update original message with reopened status
						if err := sl.UpdateTaskStatusReopened(slackMsg, taskID, task.Name, task.TaskURL, operatorName); err != nil {
							log.Error().Err(err).Int("task_id", taskID).Msg("slack update task reopened")
						}

						// Also add to thread for journal
						if err := sl.NotifyTaskReopened(slackMsg, taskID, task.Name, operatorName); err != nil {
							log.Error().Err(err).Int("task_id", taskID).Msg("slack notify task reopened")
						}
					}
				}
			} else {
				log.Debug().Int("task_id", taskID).Msg("task reopen check completed - no reopening needed")
			}

			// přidat komentář
			body := imap.CleanBody(em.Body)
			if body == "" {
				body = "(empty message)"
			}
			log.Debug().Int("task_id", taskID).Str("from", em.FromEmail).Msg("adding customer reply to task")

			partnerID, err := oc.FindOrCreatePartnerByEmail(ctx, em.FromEmail, em.FromName)
			if err != nil {
				// pokračuj i při chybě – vytvoří se bez autora
				log.Error().Err(err).Str("email", em.FromEmail).Msg("odoo partner")
			} else {
				log.Debug().Int64("partner_id", partnerID).Str("email", em.FromEmail).Msg("found or created partner")
			}

			if err := oc.MessagePostCustomer(ctx, taskIDInt64, partnerID, body); err != nil {
				log.Error().Err(err).Int("task_id", taskID).Msg("odoo message_post")
			} else {
				log.Debug().Int("task_id", taskID).Msg("customer reply posted successfully")
			}

			// Upload attachments for customer reply if any
			if len(em.Attachments) > 0 {
				log.Debug().Int("count", len(em.Attachments)).Int("task_id", taskID).Msg("processing reply attachments")
				for _, att := range em.Attachments {
					log.Debug().Str("filename", att.Filename).Str("content_type", att.ContentType).Int("size", len(att.Data)).Msg("uploading attachment")
					_, err := oc.UploadAttachment(ctx, int64(taskID), att.Filename, att.ContentType, att.Data)
					if err != nil {
						log.Error().Err(err).Str("filename", att.Filename).Int("task_id", taskID).Msg("reply attachment upload failed")
					} else {
						log.Debug().Str("filename", att.Filename).Int("task_id", taskID).Msg("reply attachment uploaded successfully")
					}
				}
			}

			_ = st.MarkProcessedEmail(em.ID)
			_ = im.MarkSeen(ctx, em.UID)
			continue
		}

		// nový ticket
		log.Debug().Str("from", em.FromEmail).Str("subject", em.Subject).Msg("creating new ticket")

		title := em.Subject
		if title == "" {
			title = "Nový požadavek"
		}

		// Debug original body content
		log.Debug().Int("original_body_length", len(em.Body)).Str("original_body_preview", truncateString(em.Body, previewLength)).Msg("original email body")
		desc := imap.CleanBody(em.Body)
		log.Debug().Int("cleaned_body_length", len(desc)).Str("cleaned_body_preview", truncateString(desc, previewLength)).Msg("cleaned email body")

		if desc == "" && len(em.Body) > 0 {
			log.Debug().Msg("CleanBody returned empty string, using original body")
			desc = em.Body // Use original body if CleanBody returns empty
		}

		log.Debug().Int("final_desc_length", len(desc)).Str("final_desc_preview", truncateString(desc, previewLength)).Msg("final description for task creation")

		partnerID, err := oc.FindOrCreatePartnerByEmail(ctx, em.FromEmail, em.FromName)
		if err != nil {
			log.Error().Err(err).Str("email", em.FromEmail).Msg("odoo partner")
		} else {
			log.Debug().Int64("partner_id", partnerID).Str("email", em.FromEmail).Msg("found or created partner for new ticket")
		}

		taskID64, err := oc.CreateTask(ctx, odoo.CreateTaskInput{
			ProjectID:         int64(cfg.Odoo.ProjectID),
			Name:              title,
			Description:       desc,
			CustomerPartnerID: partnerID,
			StageID:           cfg.Odoo.Stages.New, // Start in "Nové" stage
		})
		if err != nil {
			log.Error().Err(err).Str("title", title).Msg("odoo create task")
			continue
		}
		newTaskID := int(taskID64)
		taskURL := oc.TaskURL(cfg.Odoo.BaseURL, taskID64)

		log.Debug().Int("task_id", newTaskID).Str("url", taskURL).Msg("new task created successfully")

		// Upload attachments if any
		if len(em.Attachments) > 0 {
			log.Info().Int("count", len(em.Attachments)).Int64("task_id", taskID64).Msg("uploading attachments")
			for _, att := range em.Attachments {
				_, err := oc.UploadAttachment(ctx, taskID64, att.Filename, att.ContentType, att.Data)
				if err != nil {
					log.Error().Err(err).Str("filename", att.Filename).Int64("task_id", taskID64).Msg("attachment upload failed")
				} else {
					log.Info().Str("filename", att.Filename).Int64("task_id", taskID64).Msg("attachment uploaded")
				}
			}
		}

		// Automatic assignment to operator with least tasks
		assignedOperator := ""
		if len(cfg.App.Operators) > 0 {
			var err error
			assignedOperator, err = assignTaskToOperator(ctx, oc, taskID64, cfg)
			if err != nil {
				log.Error().Err(err).Int64("task_id", taskID64).Msg("auto assignment failed")
				assignedOperator = defaultOperatorName // Fallback message
			} else {
				log.Info().Str("operator", assignedOperator).Int64("task_id", taskID64).Msg("task auto-assigned")
			}
		} else {
			assignedOperator = "Žádný operátor k dispozici"
		}

		// Slack
		slackMsg, err := sl.NotifyNewTask(newTaskID, title, taskURL, desc, assignedOperator)
		if err != nil {
			log.Error().Err(err).Int("task_id", newTaskID).Msg("slack notify")
		} else if slackMsg != nil {
			// Store Slack message info for threading
			_ = st.StoreSlackMessage(taskID64, state.SlackMessageInfo{
				Timestamp: slackMsg.Timestamp,
				Channel:   slackMsg.Channel,
			})

			// Notify about task assignment in thread if operator was assigned
			if assignedOperator != "" && assignedOperator != defaultOperatorName && assignedOperator != "Žádný operátor k dispozici" {
				if err := sl.NotifyTaskAssigned(slackMsg, newTaskID, assignedOperator); err != nil {
					log.Error().Err(err).Int("task_id", newTaskID).Msg("slack notify task assigned")
				}
			}
		}

		// Initialize SLA tracking
		_ = slaHandler.InitializeTask(taskID64)

		// potvrzení zákazníkovi
		subj, body, err := tm.RenderNewTicket(cfg.App.TicketPrefix, newTaskID, em.FromName, desc, cfg.App.SLA.StartTimeHours, cfg.App.SLA.ResolutionTimeHours)
		if err == nil {
			if err := m.Send(em.FromEmail, subj, body); err != nil {
				log.Error().Err(err).Str("email", em.FromEmail).Msg("send confirm")
			}
		} else {
			log.Error().Err(err).Int("task_id", newTaskID).Msg("tmpl")
		}

		_ = st.MarkProcessedEmail(em.ID)
		_ = im.MarkSeen(ctx, em.UID)
	}
	return nil
}

// processOdooPublicMessages handles processing of public messages from Odoo
func processOdooPublicMessages(
	ctx context.Context,
	cfg *config.Config,
	oc *odoo.Client,
	st *state.Store,
	tm *templ.Engine,
	m *mailer.SMTPClient,
) error {
	log.Debug().Msg("processOdooPublicMessages: starting")
	lastTS := st.GetLastOdooMessageTime()
	msgs, err := oc.ListTaskMessagesSince(ctx, lastTS)
	if err != nil {
		log.Error().Err(err).Msg("processOdooPublicMessages: ListTaskMessagesSince failed")
		return err
	}
	log.Debug().Int("count", len(msgs)).Msg("processOdooPublicMessages: got messages")
	maxSeen := lastTS
	for _, mm := range msgs {
		log.Debug().Int64("msg_id", mm.ID).Int64("task_id", mm.TaskID).Bool("by_operator", mm.ByOperator).Bool("is_comment", mm.IsComment).Bool("is_public", mm.IsPublicPrefix).Msg("processOdooPublicMessages: checking message")
		
		if mm.Date.After(maxSeen) {
			maxSeen = mm.Date
		}
		if st.IsOdooMessageSent(mm.ID) {
			log.Debug().Int64("msg_id", mm.ID).Msg("processOdooPublicMessages: message already sent, skipping")
			continue
		}
		if !mm.ByOperator || !mm.IsComment || !mm.IsPublicPrefix {
			log.Debug().Int64("msg_id", mm.ID).Bool("by_operator", mm.ByOperator).Bool("is_comment", mm.IsComment).Bool("is_public", mm.IsPublicPrefix).Msg("processOdooPublicMessages: message filtered out")
			continue
		}
		
		log.Info().Int64("msg_id", mm.ID).Int64("task_id", mm.TaskID).Msg("processOdooPublicMessages: processing public message for email")
		
		task, err := oc.GetTask(ctx, mm.TaskID)
		if err != nil || task.CustomerEmail == "" {
			log.Debug().Int64("msg_id", mm.ID).Int64("task_id", mm.TaskID).Err(err).Str("customer_email", task.CustomerEmail).Msg("processOdooPublicMessages: task details missing, skipping")
			continue
		}
		subj, body, err := tm.RenderAgentReply(cfg.App.TicketPrefix, int(task.ID), task.Name, task.CustomerName, mm.BodyWithoutPrefix)
		if err != nil {
			log.Error().Err(err).Int64("task_id", task.ID).Msg("tmpl agent")
			continue
		}

		// Get attachments for this message/task
		attachments, err := oc.GetTaskAttachments(ctx, mm.TaskID)
		if err != nil {
			log.Error().Err(err).Int64("task_id", mm.TaskID).Msg("get attachments for reply")
		}

		log.Info().Int64("msg_id", mm.ID).Int64("task_id", mm.TaskID).Str("customer_email", task.CustomerEmail).Int("attachments", len(attachments)).Msg("processOdooPublicMessages: sending agent reply email")
		
		// Send email with attachments if any
		if len(attachments) > 0 {
			log.Debug().Int64("msg_id", mm.ID).Str("subject", subj).Msg("processOdooPublicMessages: sending email with attachments")
			if err := sendEmailWithAttachments(ctx, m, oc, task.CustomerEmail, subj, body, attachments); err != nil {
				log.Error().Err(err).Str("email", task.CustomerEmail).Msg("send agent reply with attachments")
			} else {
				log.Info().Int64("msg_id", mm.ID).Str("email", task.CustomerEmail).Msg("processOdooPublicMessages: email with attachments sent successfully")
				_ = st.MarkOdooMessageSent(mm.ID)
			}
		} else {
			log.Debug().Int64("msg_id", mm.ID).Str("subject", subj).Msg("processOdooPublicMessages: sending plain email")
			if err := m.Send(task.CustomerEmail, subj, body); err != nil {
				log.Error().Err(err).Str("email", task.CustomerEmail).Msg("send agent")
			} else {
				log.Info().Int64("msg_id", mm.ID).Str("email", task.CustomerEmail).Msg("processOdooPublicMessages: email sent successfully")
				_ = st.MarkOdooMessageSent(mm.ID)
			}
		}
	}
	if maxSeen.After(lastTS) {
		_ = st.SetLastOdooMessageTime(maxSeen)
	}
	return nil
}

// processCompletedTasks handles processing of completed tasks
func processCompletedTasks(
	ctx context.Context,
	cfg *config.Config,
	oc *odoo.Client,
	st *state.Store,
	tm *templ.Engine,
	m *mailer.SMTPClient,
	sl *slack.Client,
	basicTasks []*odoo.Task,
) error {
	log.Debug().Int("count", len(basicTasks)).Msg("processCompletedTasks: starting")
	for _, basicTask := range basicTasks {
		log.Debug().Int64("task_id", basicTask.ID).Str("name", basicTask.Name).Msg("processCompletedTasks: checking task")
		if st.IsTaskClosedNotified(basicTask.ID) {
			log.Debug().Int64("task_id", basicTask.ID).Msg("processCompletedTasks: task already notified, skipping")
			continue
		}
		if !oc.IsTaskDone(basicTask, cfg.App.DoneStageIDs) {
			log.Debug().Int64("task_id", basicTask.ID).Msg("processCompletedTasks: task not done, skipping")
			continue
		}

		log.Info().Int64("task_id", basicTask.ID).Str("name", basicTask.Name).Msg("processCompletedTasks: processing completed task")

		// Only get full task details (with customer email) for tasks we actually need to process
		t, err := oc.GetTask(ctx, basicTask.ID)
		if err != nil || t.CustomerEmail == "" {
			continue
		}

		// Update Slack message and notify in thread about task completion
		if parentMsg, err := st.GetSlackMessage(t.ID); err == nil && parentMsg != nil {
			slackMsg := &slack.Message{
				Timestamp: parentMsg.Timestamp,
				Channel:   parentMsg.Channel,
			}

			// Update original message with completion status
			assignedName := t.AssignedUserName
			if assignedName == "" {
				assignedName = defaultOperatorName
			}
			if err := sl.UpdateTaskStatusCompleted(slackMsg, int(t.ID), t.Name, t.TaskURL, assignedName); err != nil {
				log.Error().Err(err).Int64("task_id", t.ID).Msg("slack update task completed")
			}

			// Also add to thread for journal
			if err := sl.NotifyTaskCompleted(slackMsg, int(t.ID), t.Name); err != nil {
				log.Error().Err(err).Int64("task_id", t.ID).Msg("slack notify task completed")
			}
		}

		log.Info().Int64("task_id", t.ID).Str("customer_email", t.CustomerEmail).Msg("processCompletedTasks: sending completion email")
		
		subj, body, err := tm.RenderTicketClosed(cfg.App.TicketPrefix, int(t.ID), t.TaskURL, t.CustomerName)
		if err != nil {
			log.Error().Err(err).Int64("task_id", t.ID).Msg("tmpl close")
			continue
		}
		
		log.Debug().Int64("task_id", t.ID).Str("subject", subj).Msg("processCompletedTasks: sending email")
		
		if err := m.Send(t.CustomerEmail, subj, body); err != nil {
			log.Error().Err(err).Str("email", t.CustomerEmail).Msg("send close")
		} else {
			log.Info().Int64("task_id", t.ID).Str("email", t.CustomerEmail).Msg("processCompletedTasks: email sent successfully")
			_ = st.MarkTaskClosedNotified(t.ID)
			// Clear reopened notification flag since task is now closed
			_ = st.ClearTaskReopenedNotified(t.ID)
		}
	}
	return nil
}

// processReopenedTasks handles processing of reopened tasks
func processReopenedTasks(
	ctx context.Context,
	cfg *config.Config,
	oc *odoo.Client,
	st *state.Store,
	sl *slack.Client,
	basicTasks []*odoo.Task,
) error {
	log.Debug().Int("count", len(basicTasks)).Msg("processReopenedTasks: starting")
	for _, basicTask := range basicTasks {
		log.Debug().Int64("task_id", basicTask.ID).Str("name", basicTask.Name).Msg("processReopenedTasks: checking task")
		// Skip if task is currently done or if we already notified about reopening
		if oc.IsTaskDone(basicTask, cfg.App.DoneStageIDs) || st.IsTaskReopenedNotified(basicTask.ID) {
			continue
		}

		// Only process if task was previously closed (has closed notification)
		if !st.IsTaskClosedNotified(basicTask.ID) {
			continue
		}

		// Get full task details for reopened task
		t, err := oc.GetTask(ctx, basicTask.ID)
		if err != nil || t.CustomerEmail == "" {
			continue
		}

		log.Info().Int64("task_id", t.ID).Str("name", t.Name).Msg("processing reopened task")

		// Update Slack message and notify in thread about task reopening
		if parentMsg, err := st.GetSlackMessage(t.ID); err == nil && parentMsg != nil {
			slackMsg := &slack.Message{
				Timestamp: parentMsg.Timestamp,
				Channel:   parentMsg.Channel,
			}

			// Update original message with reopened status
			assignedName := t.AssignedUserName
			if assignedName == "" {
				assignedName = defaultOperatorName
			}
			if err := sl.UpdateTaskStatusReopened(slackMsg, int(t.ID), t.Name, t.TaskURL, assignedName); err != nil {
				log.Error().Err(err).Int64("task_id", t.ID).Msg("slack update task reopened")
			}

			// Notify @channel in thread about reopened task
			if err := sl.NotifyTaskReopened(slackMsg, int(t.ID), t.Name, assignedName); err != nil {
				log.Error().Err(err).Int64("task_id", t.ID).Msg("slack notify task reopened")
			}
		}

		// Mark as notified to avoid duplicate notifications
		_ = st.MarkTaskReopenedNotified(t.ID)
	}
	return nil
}

func processOdooEvents(
	ctx context.Context,
	cfg *config.Config,
	oc *odoo.Client,
	st *state.Store,
	tm *templ.Engine,
	m *mailer.SMTPClient,
	sl *slack.Client,
) error {
	log.Debug().Msg("processOdooEvents: starting")

	// Process public messages (comments -> emails to customers)
	log.Debug().Msg("processOdooEvents: calling processOdooPublicMessages")
	if err := processOdooPublicMessages(ctx, cfg, oc, st, tm, m); err != nil {
		log.Error().Err(err).Msg("processOdooEvents: processOdooPublicMessages failed")
		return err
	}

	// Get recently changed tasks for processing completed and reopened tasks
	log.Debug().Msg("processOdooEvents: getting recently changed tasks")
	basicTasks, err := oc.ListRecentlyChangedTasksForSLA(ctx, time.Now().Add(-48*time.Hour))
	if err != nil {
		log.Error().Err(err).Msg("processOdooEvents: ListRecentlyChangedTasksForSLA failed")
		return err
	}
	log.Debug().Int("count", len(basicTasks)).Msg("processOdooEvents: got basic tasks")

	// Process completed tasks
	log.Debug().Msg("processOdooEvents: calling processCompletedTasks")
	if err := processCompletedTasks(ctx, cfg, oc, st, tm, m, sl, basicTasks); err != nil {
		log.Error().Err(err).Msg("processOdooEvents: processCompletedTasks failed")
		return err
	}

	// Process reopened tasks
	log.Debug().Msg("processOdooEvents: calling processReopenedTasks")
	if err := processReopenedTasks(ctx, cfg, oc, st, sl, basicTasks); err != nil {
		log.Error().Err(err).Msg("processOdooEvents: processReopenedTasks failed")
		return err
	}

	log.Debug().Msg("processOdooEvents: completed successfully")
	return nil
}

// isExcludedEmail checks if the email address should be excluded from ticket creation
func isExcludedEmail(email string, excludedEmails []string) bool {
	email = strings.ToLower(email)
	for _, excluded := range excludedEmails {
		if strings.ToLower(excluded) == email {
			return true
		}
	}
	return false
}

// assignTaskToOperator assigns task to operator with fewest assigned tasks (round-robin)
func assignTaskToOperator(ctx context.Context, oc *odoo.Client, taskID int64, cfg *config.Config) (string, error) {
	log.Debug().Int64("task_id", taskID).Strs("operators", cfg.App.Operators).Msg("starting operator assignment")

	// Get task counts for all operators
	counts, err := oc.GetTaskCounts(ctx, int64(cfg.Odoo.ProjectID), cfg.App.Operators)
	if err != nil {
		log.Error().Err(err).Int64("task_id", taskID).Msg("failed to get task counts")
		return "", err
	}

	// Find operator with minimum tasks
	minCount := -1
	selectedOperator := ""
	for _, operator := range cfg.App.Operators {
		count := counts[operator]
		log.Debug().Str("operator", operator).Int("count", count).Msg("operator task count")
		if minCount == -1 || count < minCount {
			minCount = count
			selectedOperator = operator
		}
	}

	log.Debug().Str("selected_operator", selectedOperator).Int("min_count", minCount).Msg("selected operator with minimum tasks")

	if selectedOperator == "" {
		return "", fmt.Errorf("no available operators")
	}

	// Assign task to selected operator
	err = oc.AssignTask(ctx, taskID, selectedOperator)
	if err != nil {
		return "", err
	}

	// Move task to "Přiřazeno" stage
	err = oc.SetTaskStage(ctx, taskID, cfg.Odoo.Stages.Assigned)
	if err != nil {
		return "", err
	}

	return selectedOperator, nil
}

// sendEmailWithAttachments sends email with Odoo attachments
func sendEmailWithAttachments(
	ctx context.Context,
	m *mailer.SMTPClient,
	oc *odoo.Client,
	to, subject, body string,
	odooAttachments []odoo.Attachment,
) error {
	if len(odooAttachments) == 0 {
		return m.Send(to, subject, body)
	}

	var attachments []mailer.Attachment
	for _, odooAtt := range odooAttachments {
		// Download attachment data from Odoo
		data, err := oc.DownloadAttachment(ctx, odooAtt.ID)
		if err != nil {
			log.Error().Err(err).Int64("attachment_id", odooAtt.ID).Msg("download attachment failed")
			continue // Skip this attachment but continue with others
		}

		attachments = append(attachments, mailer.Attachment{
			Filename:    odooAtt.Name,
			ContentType: odooAtt.MimeType,
			Data:        data,
		})
	}

	return m.SendWithAttachments(to, subject, body, attachments)
}

// truncateString truncates a string to maxLength characters, adding "..." if truncated
func truncateString(s string, maxLength int) string {
	if len(s) <= maxLength {
		return s
	}
	if maxLength < minTruncateLength {
		return s[:maxLength]
	}
	return s[:maxLength-3] + "..."
}
