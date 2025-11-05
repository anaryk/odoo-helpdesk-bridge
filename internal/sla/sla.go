// Package sla provides Service Level Agreement monitoring and violation detection for tickets.
package sla

import (
	"context"
	"fmt"
	"time"

	"github.com/anaryk/odoo-helpdesk-bridge/internal/config"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/odoo"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/slack"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/state"
)

const (
	// extraBufferHours provides additional buffer time for SLA violation checking
	extraBufferHours = 24
)

// Handler manages SLA monitoring and violations
type Handler struct {
	cfg         *config.Config
	odooClient  *odoo.Client
	slackClient *slack.Client
	state       *state.Store
}

// New creates a new SLA handler
func New(cfg *config.Config, odooClient *odoo.Client, slackClient *slack.Client, state *state.Store) *Handler {
	return &Handler{
		cfg:         cfg,
		odooClient:  odooClient,
		slackClient: slackClient,
		state:       state,
	}
}

// InitializeTask creates initial SLA state when a new task is created
func (h *Handler) InitializeTask(taskID int64) error {
	slaState := state.SLAState{
		TaskID:    taskID,
		CreatedAt: time.Now(),
	}
	return h.state.StoreSLAState(slaState)
}

// CheckSLAViolations checks for SLA violations and sends notifications
func (h *Handler) CheckSLAViolations(ctx context.Context) error {
	// Get recent tasks that might have SLA violations (optimized for SLA checking)
	since := time.Now().Add(-time.Duration(h.cfg.App.SLA.ResolutionTimeHours+extraBufferHours) * time.Hour)
	tasks, err := h.odooClient.ListRecentlyChangedTasksForSLA(ctx, int64(h.cfg.Odoo.ProjectID), since)
	if err != nil {
		return fmt.Errorf("failed to get recent tasks: %w", err)
	}

	for _, task := range tasks {
		if err := h.checkTaskSLA(ctx, task); err != nil {
			// Log error but continue with other tasks
			continue
		}
	}

	return nil
}

func (h *Handler) checkTaskSLA(ctx context.Context, task *odoo.Task) error {
	slaState, err := h.state.GetSLAState(task.ID)
	if err != nil {
		return err
	}

	// If no SLA state exists, create one (for existing tasks)
	if slaState == nil {
		slaState = &state.SLAState{
			TaskID:    task.ID,
			CreatedAt: time.Now(), // We don't know the real creation time for existing tasks
		}
	}

	now := time.Now()
	updated := false

	// Check if task is done
	isCompleted := h.odooClient.IsTaskDone(task, h.cfg.App.DoneStageIDs)
	if isCompleted && slaState.CompletedAt == nil {
		slaState.CompletedAt = &now
		updated = true
	}

	// Check if task was started (moved from initial stage)
	// For simplicity, we assume if it's not "new" stage, it's started
	isStarted := !h.isNewStage(task.StageName)
	if isStarted && slaState.StartedAt == nil {
		slaState.StartedAt = &now
		updated = true
	}

	// Check start time SLA
	if !slaState.StartSLABreach && slaState.StartedAt == nil {
		startDeadline := slaState.CreatedAt.Add(time.Duration(h.cfg.App.SLA.StartTimeHours) * time.Hour)
		if now.After(startDeadline) {
			slaState.StartSLABreach = true
			updated = true

			// Add label to Odoo task
			if err := h.addSLALabel(ctx, task.ID, "SLA_START_BREACH"); err != nil {
				return err
			}

			// Send Slack notification
			if err := h.notifySlackSLAViolation(task, "start_time"); err != nil {
				return err
			}
		}
	}

	// Check resolution time SLA
	if !slaState.EndSLABreach && slaState.CompletedAt == nil {
		resolutionDeadline := slaState.CreatedAt.Add(time.Duration(h.cfg.App.SLA.ResolutionTimeHours) * time.Hour)
		if now.After(resolutionDeadline) {
			slaState.EndSLABreach = true
			updated = true

			// Add label to Odoo task
			if err := h.addSLALabel(ctx, task.ID, "SLA_RESOLUTION_BREACH"); err != nil {
				return err
			}

			// Send Slack notification
			if err := h.notifySlackSLAViolation(task, "resolution_time"); err != nil {
				return err
			}
		}
	}

	// Update state if changed
	if updated {
		return h.state.StoreSLAState(*slaState)
	}

	return nil
}

func (h *Handler) isNewStage(stageName string) bool {
	// Simple heuristic to determine if a stage is "new"
	// You might want to make this configurable
	newStages := []string{"new", "nový", "draft", "návrh", "backlog"}
	for _, stage := range newStages {
		if stage == stageName {
			return true
		}
	}
	return false
}

func (h *Handler) addSLALabel(ctx context.Context, taskID int64, label string) error {
	// This would need to be implemented in the Odoo client
	// For now, we'll add a comment to the task instead
	return h.odooClient.MessagePostCustomer(ctx, taskID, 0, fmt.Sprintf("[SYSTEM] SLA Label: %s", label))
}

func (h *Handler) notifySlackSLAViolation(task *odoo.Task, violationType string) error {
	// Get Slack message for threading
	slackMsg, err := h.state.GetSlackMessage(task.ID)
	if err != nil || slackMsg == nil {
		// No Slack message found, can't thread
		return nil
	}

	parentMsg := &slack.Message{
		Timestamp: slackMsg.Timestamp,
		Channel:   slackMsg.Channel,
	}

	return h.slackClient.NotifySLAViolation(parentMsg, int(task.ID), task.Name, violationType)
}
