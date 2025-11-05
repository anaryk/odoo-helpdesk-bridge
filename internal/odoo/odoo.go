// Package odoo provides integration with Odoo ERP system for task management and API interactions.
package odoo

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	// previewMaxLength defines maximum length for task description preview
	previewMaxLength = 100

	// defaultQueryLimit defines default limit for Odoo API queries
	defaultQueryLimit = 200

	// minFieldLength for partner/user data validation
	minFieldLength = 2
)

// Config holds Odoo server connection configuration.
type Config struct {
	URL     string
	DB      string
	User    string
	Pass    string
	Timeout time.Duration
}

// Client represents an authenticated Odoo API client.
type Client struct {
	cfg  Config
	uid  int64
	http *http.Client
}

// NewClient creates a new authenticated Odoo client with the provided configuration.
func NewClient(ctx context.Context, cfg Config) (*Client, error) {
	cl := &Client{
		cfg:  cfg,
		http: &http.Client{Timeout: cfg.Timeout},
	}
	uid, err := cl.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	cl.uid = uid
	return cl, nil
}

// --- auth & rpc ---

func (c *Client) authenticate(ctx context.Context) (int64, error) {
	var uid int64
	err := c.rpc(ctx, "/jsonrpc", map[string]any{
		"service": "common",
		"method":  "authenticate",
		"args":    []any{c.cfg.DB, c.cfg.User, c.cfg.Pass, map[string]any{}},
	}, &uid)
	return uid, err
}

func (c *Client) execKW(ctx context.Context, model, method string, args []any, kwargs map[string]any, result any) error {
	payload := map[string]any{
		"service": "object",
		"method":  "execute_kw",
		"args":    []any{c.cfg.DB, c.uid, c.cfg.Pass, model, method, args, kwargs},
	}
	return c.rpc(ctx, "/jsonrpc", payload, result)
}

func (c *Client) rpc(ctx context.Context, path string, call map[string]any, result any) error {
	reqBody := map[string]any{
		"jsonrpc": "2.0",
		"method":  "call",
		"params":  call,
		"id":      time.Now().UnixNano(),
	}
	b, _ := json.Marshal(reqBody)
	req, _ := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.cfg.URL, "/")+path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Error().Err(err).Msg("failed to close response body")
		}
	}()
	var r struct {
		Result any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Data    any    `json:"data"`
			Message string `json:"message"`
		} `json:"error"`
	}
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&r); err != nil {
		return err
	}
	if r.Error != nil {
		return errors.New(r.Error.Message)
	}
	if result != nil {
		j, _ := json.Marshal(r.Result)
		_ = json.Unmarshal(j, result)
	}
	return nil
}

// --- domain types ---

// CreateTaskInput holds the parameters for creating a new task in Odoo.
type CreateTaskInput struct {
	ProjectID         int64
	Name              string
	Description       string
	CustomerPartnerID int64
	StageID           int64
}

// CreateTask creates a new task in Odoo with the provided input parameters.
func (c *Client) CreateTask(ctx context.Context, in CreateTaskInput) (int64, error) {
	log.Debug().Str("name", in.Name).Int64("project_id", in.ProjectID).Int64("partner_id", in.CustomerPartnerID).Int64("stage_id", in.StageID).Int("description_length", len(in.Description)).Msg("creating new task")

	if len(in.Description) > 0 {
		preview := in.Description
		if len(preview) > previewMaxLength {
			preview = preview[:97] + "..."
		}
		log.Debug().Str("description_preview", preview).Msg("task description content")
	} else {
		log.Warn().Msg("task description is empty")
	}

	fields := map[string]any{
		"name":        in.Name,
		"project_id":  in.ProjectID,
		"description": in.Description,
	}
	if in.CustomerPartnerID > 0 {
		fields["partner_id"] = in.CustomerPartnerID
	}
	if in.StageID > 0 {
		fields["stage_id"] = in.StageID
	}
	var id int64
	err := c.execKW(ctx, "project.task", "create", []any{fields}, nil, &id)
	if err != nil {
		return id, err
	}

	log.Debug().Int64("task_id", id).Msg("task created successfully")

	// Add customer as follower (for notifications)
	if in.CustomerPartnerID > 0 {
		if followerErr := c.AddFollower(ctx, id, in.CustomerPartnerID); followerErr != nil {
			log.Debug().Err(followerErr).Int64("task_id", id).Int64("partner_id", in.CustomerPartnerID).Msg("failed to add follower")
		} else {
			log.Debug().Int64("task_id", id).Int64("partner_id", in.CustomerPartnerID).Msg("customer added as follower")
		}
	}
	return id, err
}

// AddFollower adds a partner as a follower to the specified task.
func (c *Client) AddFollower(ctx context.Context, taskID, partnerID int64) error {
	// message_subscribe
	var ok bool
	return c.execKW(ctx, "project.task", "message_subscribe", []any{taskID, []int64{partnerID}}, nil, &ok)
}

// FindOrCreatePartnerByEmail finds an existing partner by email or creates a new one.
func (c *Client) FindOrCreatePartnerByEmail(ctx context.Context, email, name string) (int64, error) {
	var ids []int64
	err := c.execKW(ctx, "res.partner", "search", []any{[][]any{{"email", "=", email}}}, map[string]any{"limit": 1}, &ids)
	if err != nil {
		return 0, err
	}
	if len(ids) > 0 {
		return ids[0], nil
	}
	// create
	var id int64
	err = c.execKW(ctx, "res.partner", "create", []any{map[string]any{
		"name":  ifEmpty(name, email),
		"email": email,
	}}, nil, &id)
	return id, err
}

func ifEmpty(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

// TaskURL generates a URL for accessing a task in the Odoo web interface.
func (c *Client) TaskURL(base string, id int64) string {
	base = strings.TrimRight(base, "/")
	return base + "/web#id=" + itoa(int(id)) + "&model=project.task&view_type=form"
}

func itoa(v int) string {
	buf := [20]byte{}
	i := len(buf)
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// --- Messages (chatter) ---

// MessagePostCustomer posts a message to a task as a customer communication.
func (c *Client) MessagePostCustomer(ctx context.Context, taskID, customerPartnerID int64, body string) error {
	// public comment -> goes to followers
	var ok any
	return c.execKW(ctx, "project.task", "message_post", []any{taskID}, map[string]any{
		"body":          body,
		"message_type":  "comment",
		"subtype_xmlid": "mail.mt_comment",
		"author_id":     customerPartnerID, // partner
	}, &ok)
}

// TaskMessage represents a message associated with a project task for operator message polling.
type TaskMessage struct {
	ID                int64
	TaskID            int64
	Body              string
	BodyWithoutPrefix string
	Date              time.Time
	ByOperator        bool
	IsComment         bool
	IsPublicPrefix    bool // starts with [public]
}

// ListTaskMessagesSince retrieves task messages that have been created since the specified time for a specific project.
func (c *Client) ListTaskMessagesSince(ctx context.Context, projectID int64, since time.Time) ([]TaskMessage, error) {
	log.Debug().Int64("project_id", projectID).Time("since", since).Msg("fetching task messages for specific project")

	// First get task IDs from the specific project
	var taskIDs []int64
	taskDomain := [][]any{{"project_id", "=", projectID}}
	if err := c.execKW(ctx, "project.task", "search", []any{taskDomain}, nil, &taskIDs); err != nil {
		return nil, fmt.Errorf("failed to get tasks for project %d: %w", projectID, err)
	}
	if len(taskIDs) == 0 {
		log.Debug().Int64("project_id", projectID).Msg("no tasks found in project")
		return nil, nil
	}

	// search mail.message by model=project.task and res_id in task_ids and date > since
	var ids []int64
	domain := [][]any{
		{"model", "=", "project.task"},
		{"res_id", "in", taskIDs},
	}
	if !since.IsZero() {
		domain = append(domain, []any{"date", ">", since.UTC().Format("2006-01-02 15:04:05")})
	}
	if err := c.execKW(ctx, "mail.message", "search", []any{domain}, map[string]any{"order": "date asc", "limit": defaultQueryLimit}, &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []map[string]any
	if err := c.execKW(ctx, "mail.message", "read", []any{ids, []string{"id", "res_id", "body", "date", "message_type", "subtype_id", "author_id"}}, nil, &rows); err != nil {
		return nil, err
	}
	out := make([]TaskMessage, 0, len(rows))
	for _, r := range rows {
		id := toInt64(r["id"])
		taskID := toInt64(r["res_id"])
		body := htmlToText(str(r["body"]))
		date := parseOdooTime(str(r["date"]))
		msgType := str(r["message_type"]) // "comment", "notification", ...
		isComment := msgType == "comment"
		authorPair := anySlice(r["author_id"])
		var authorPartnerID int64
		if len(authorPair) >= 1 {
			authorPartnerID = toInt64(authorPair[0])
		}
		byOperator := c.partnerLooksLikeOperator(ctx, authorPartnerID)

		trim := strings.TrimSpace(body)
		isPublicPrefix := strings.HasPrefix(strings.ToLower(trim), "[public]")
		bodyWithout := strings.TrimSpace(strings.TrimPrefix(trim, "[public]"))
		out = append(out, TaskMessage{
			ID: id, TaskID: taskID, Body: body, BodyWithoutPrefix: bodyWithout,
			Date: date, ByOperator: byOperator, IsComment: isComment, IsPublicPrefix: isPublicPrefix,
		})
	}
	return out, nil
}

func (c *Client) partnerLooksLikeOperator(ctx context.Context, partnerID int64) bool {
	// if partner has a user (res.users) -> operator
	var ids []int64
	_ = c.execKW(ctx, "res.users", "search", []any{[][]any{{"partner_id", "=", partnerID}, {"active", "=", true}}}, map[string]any{"limit": 1}, &ids)
	return len(ids) > 0
}

func parseOdooTime(v string) time.Time {
	// Odoo 16: "2006-01-02 15:04:05"
	t, _ := time.ParseInLocation("2006-01-02 15:04:05", v, time.UTC)
	return t
}

func anySlice(v any) []any {
	if v == nil {
		return nil
	}
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}
func toInt64(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case json.Number:
		i, _ := t.Int64()
		return i
	default:
		return 0
	}
}
func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func htmlToText(s string) string {
	// very naive: strip basic tags
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	return strings.TrimSpace(stripTags(s))
}

func stripTags(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// --- tasks ---

// Task represents a project task in Odoo with associated metadata.
type Task struct {
	ID               int64
	Name             string
	StageID          int64
	StageName        string
	CustomerEmail    string
	CustomerName     string
	TaskURL          string
	AssignedUserID   int64
	AssignedUserName string
}

// GetTask retrieves a task by its ID from Odoo.
func (c *Client) GetTask(ctx context.Context, id int64) (*Task, error) {
	var rows []map[string]any
	if err := c.execKW(ctx, "project.task", "read", []any{[]int64{id}, []string{"id", "name", "stage_id", "partner_id", "user_ids"}}, nil, &rows); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.New("task not found")
	}
	r := rows[0]
	stagePair := anySlice(r["stage_id"])
	var stageID int64
	var stageName string
	if len(stagePair) >= minFieldLength {
		stageID = toInt64(stagePair[0])
		stageName = str(stagePair[1])
	}
	// Get partner (customer) email and name
	partnerPair := anySlice(r["partner_id"])
	var email, pname string
	if len(partnerPair) >= minFieldLength {
		partnerID := toInt64(partnerPair[0])
		pname = str(partnerPair[1])
		if partnerID > 0 {
			email = c.partnerEmailName(ctx, partnerID)
		}
	}

	// Get assigned user info (user_ids is Many2many, take first user if any)
	userIds := anySlice(r["user_ids"])
	var assignedUserID int64
	var assignedUserName string
	if len(userIds) > 0 {
		assignedUserID = toInt64(userIds[0])
		// Get user name by ID
		var userRows []map[string]any
		if err := c.execKW(ctx, "res.users", "read", []any{[]int64{assignedUserID}, []string{"name"}}, nil, &userRows); err == nil && len(userRows) > 0 {
			assignedUserName = str(userRows[0]["name"])
		}
	}

	t := &Task{
		ID:               toInt64(r["id"]),
		Name:             str(r["name"]),
		StageID:          stageID,
		StageName:        stageName,
		CustomerEmail:    email,
		CustomerName:     pname,
		TaskURL:          c.TaskURL(c.cfg.URL, toInt64(r["id"])),
		AssignedUserID:   assignedUserID,
		AssignedUserName: assignedUserName,
	}
	return t, nil
}

func (c *Client) partnerEmailName(ctx context.Context, id int64) string {
	var rows []map[string]any
	_ = c.execKW(ctx, "res.partner", "read", []any{[]int64{id}, []string{"email"}}, nil, &rows)
	if len(rows) == 0 {
		return ""
	}
	return str(rows[0]["email"])
}

// ListRecentlyChangedTasks retrieves tasks that have been modified since the specified time for a specific project.
func (c *Client) ListRecentlyChangedTasks(ctx context.Context, projectID int64, since time.Time) ([]*Task, error) {
	log.Debug().Int64("project_id", projectID).Time("since", since).Msg("fetching recently changed tasks for specific project")
	var ids []int64
	domain := [][]any{
		{"write_date", ">", since.UTC().Format("2006-01-02 15:04:05")},
		{"project_id", "=", projectID},
	}
	if err := c.execKW(ctx, "project.task", "search", []any{domain}, map[string]any{"limit": defaultQueryLimit}, &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []map[string]any
	if err := c.execKW(ctx, "project.task", "read", []any{ids, []string{"id", "name", "stage_id", "partner_id", "user_ids"}}, nil, &rows); err != nil {
		return nil, err
	}
	var out []*Task
	for _, r := range rows {
		stagePair := anySlice(r["stage_id"])
		var stageID int64
		var stageName string
		if len(stagePair) >= minFieldLength {
			stageID = toInt64(stagePair[0])
			stageName = str(stagePair[1])
		}

		// Get partner (customer) email and name
		partnerPair := anySlice(r["partner_id"])
		var email, pname string
		if len(partnerPair) >= minFieldLength {
			partnerID := toInt64(partnerPair[0])
			pname = str(partnerPair[1])
			if partnerID > 0 {
				email = c.partnerEmailName(ctx, partnerID)
			}
		}

		// Get assigned user info (user_ids is Many2many, take first user if any)
		userIds := anySlice(r["user_ids"])
		var assignedUserID int64
		var assignedUserName string
		if len(userIds) > 0 {
			assignedUserID = toInt64(userIds[0])
			// Get user name by ID
			var userRows []map[string]any
			if err := c.execKW(ctx, "res.users", "read", []any{[]int64{assignedUserID}, []string{"name"}}, nil, &userRows); err == nil && len(userRows) > 0 {
				assignedUserName = str(userRows[0]["name"])
			}
		}

		out = append(out, &Task{
			ID:      toInt64(r["id"]),
			Name:    str(r["name"]),
			StageID: stageID, StageName: stageName,
			CustomerEmail: email, CustomerName: pname,
			TaskURL:          c.TaskURL(c.cfg.URL, toInt64(r["id"])),
			AssignedUserID:   assignedUserID,
			AssignedUserName: assignedUserName,
		})
	}
	return out, nil
}

// ListRecentlyChangedTasksForSLA returns tasks changed since given time, optimized for SLA checking (no customer emails)
func (c *Client) ListRecentlyChangedTasksForSLA(ctx context.Context, projectID int64, since time.Time) ([]*Task, error) {
	log.Debug().Int64("project_id", projectID).Time("since", since).Msg("fetching recently changed tasks for specific project")
	var ids []int64
	domain := [][]any{
		{"write_date", ">", since.UTC().Format("2006-01-02 15:04:05")},
		{"project_id", "=", projectID},
	}
	if err := c.execKW(ctx, "project.task", "search", []any{domain}, map[string]any{"limit": defaultQueryLimit}, &ids); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []map[string]any
	if err := c.execKW(ctx, "project.task", "read", []any{ids, []string{"id", "name", "stage_id"}}, nil, &rows); err != nil {
		return nil, err
	}
	var out []*Task
	for _, r := range rows {
		stagePair := anySlice(r["stage_id"])
		var stageID int64
		var stageName string
		if len(stagePair) >= minFieldLength {
			stageID = toInt64(stagePair[0])
			stageName = str(stagePair[1])
		}

		out = append(out, &Task{
			ID:        toInt64(r["id"]),
			Name:      str(r["name"]),
			StageID:   stageID,
			StageName: stageName,
			TaskURL:   c.TaskURL(c.cfg.URL, toInt64(r["id"])),
		})
	}
	return out, nil
}

// IsTaskDone checks if a task is in a done stage based on the provided stage IDs.
func (c *Client) IsTaskDone(t *Task, doneStageIDs []int64) bool {
	if len(doneStageIDs) > 0 {
		for _, id := range doneStageIDs {
			if id == t.StageID {
				return true
			}
		}
		return false
	}
	// fallback: heuristic by name
	name := strings.ToLower(t.StageName)
	return strings.Contains(name, "done") || strings.Contains(name, "hotovo") || strings.Contains(name, "closed") || strings.Contains(name, "resolved")
}

// SetTaskStage updates the stage of a task
func (c *Client) SetTaskStage(ctx context.Context, taskID, stageID int64) error {
	var ok bool
	return c.execKW(ctx, "project.task", "write", []any{[]int64{taskID}, map[string]any{"stage_id": stageID}}, nil, &ok)
}

// AssignTask assigns a task to a user
func (c *Client) AssignTask(ctx context.Context, taskID int64, userEmail string) error {
	log.Debug().Str("user_email", userEmail).Int64("task_id", taskID).Msg("searching for user to assign task")

	// Find user by email
	var userIDs []int64
	err := c.execKW(ctx, "res.users", "search", []any{[][]any{{"login", "=", userEmail}}}, map[string]any{"limit": 1}, &userIDs)
	if err != nil {
		log.Error().Err(err).Str("user_email", userEmail).Msg("error searching for user")
		return err
	}
	if len(userIDs) == 0 {
		log.Error().Str("user_email", userEmail).Msg("user not found in Odoo")
		return fmt.Errorf("user not found: %s", userEmail)
	}

	log.Debug().Int64("user_id", userIDs[0]).Str("user_email", userEmail).Int64("task_id", taskID).Msg("found user, assigning task")

	// Use user_ids field with Many2many format (more reliable than user_id)
	var ok bool
	updateFields := map[string]any{
		"user_ids": [][]any{[]any{6, 0, userIDs}}, // Replace existing assignees
	}

	err = c.execKW(ctx, "project.task", "write", []any{[]int64{taskID}, updateFields}, nil, &ok)
	if err != nil {
		log.Error().Err(err).Int64("task_id", taskID).Int64("user_id", userIDs[0]).Msg("task assignment failed")
		return err
	}

	log.Debug().Int64("task_id", taskID).Int64("user_id", userIDs[0]).Bool("success", ok).Msg("task assignment completed")

	return nil
}

// GetTaskCounts returns count of tasks assigned to each operator
func (c *Client) GetTaskCounts(ctx context.Context, projectID int64, operatorEmails []string) (map[string]int, error) {
	log.Debug().Int64("project_id", projectID).Strs("operators", operatorEmails).Msg("getting task counts for operators")
	counts := make(map[string]int)

	for _, email := range operatorEmails {
		// Find user ID
		var userIDs []int64
		err := c.execKW(ctx, "res.users", "search", []any{[][]any{{"login", "=", email}}}, map[string]any{"limit": 1}, &userIDs)
		if err != nil {
			log.Error().Err(err).Str("operator_email", email).Msg("error searching for operator user")
			continue
		}
		if len(userIDs) == 0 {
			log.Warn().Str("operator_email", email).Msg("operator user not found in Odoo")
			counts[email] = 0
			continue
		}

		// Count open tasks for this user
		var taskIDs []int64
		err = c.execKW(ctx, "project.task", "search", []any{[][]any{
			{"project_id", "=", projectID},
			{"user_ids", "in", userIDs[0]},
			{"stage_id.fold", "=", false}, // Not folded (open tasks)
		}}, nil, &taskIDs)
		if err != nil {
			log.Error().Err(err).Str("operator_email", email).Int64("user_id", userIDs[0]).Msg("error counting tasks for operator")
			counts[email] = 0
			continue
		}
		counts[email] = len(taskIDs)
		log.Debug().Str("operator_email", email).Int64("user_id", userIDs[0]).Int("task_count", len(taskIDs)).Msg("counted tasks for operator")
	}

	log.Debug().Any("counts", counts).Msg("task counts for all operators")
	return counts, nil
}

// Attachment represents an attachment in Odoo with metadata.
type Attachment struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	MimeType string `json:"mimetype"`
	Size     int64  `json:"file_size"`
}

// UploadAttachment uploads an attachment to a task
func (c *Client) UploadAttachment(ctx context.Context, taskID int64, filename string, contentType string, data []byte) (*Attachment, error) {
	log.Debug().Int64("task_id", taskID).Str("filename", filename).Str("content_type", contentType).Int("data_size", len(data)).Msg("uploading attachment to Odoo")

	// Encode data to base64
	encodedData := base64.StdEncoding.EncodeToString(data)
	log.Debug().Int("encoded_size", len(encodedData)).Msg("encoded attachment data")

	// Create attachment record
	attachmentData := map[string]any{
		"name":      filename,
		"datas":     encodedData,
		"res_model": "project.task",
		"res_id":    taskID,
		"mimetype":  contentType,
		"type":      "binary",
	}

	var attachmentID int64
	err := c.execKW(ctx, "ir.attachment", "create", []any{attachmentData}, nil, &attachmentID)
	if err != nil {
		log.Error().Err(err).Int64("task_id", taskID).Str("filename", filename).Msg("failed to create attachment in Odoo")
		return nil, err
	}

	log.Debug().Int64("attachment_id", attachmentID).Int64("task_id", taskID).Str("filename", filename).Msg("attachment uploaded successfully to Odoo")

	return &Attachment{
		ID:       attachmentID,
		Name:     filename,
		MimeType: contentType,
		Size:     int64(len(data)),
	}, nil
}

// GetTaskAttachments retrieves attachments for a task
func (c *Client) GetTaskAttachments(ctx context.Context, taskID int64) ([]Attachment, error) {
	var attachmentIDs []int64
	err := c.execKW(ctx, "ir.attachment", "search", []any{[][]any{
		{"res_model", "=", "project.task"},
		{"res_id", "=", taskID},
	}}, nil, &attachmentIDs)
	if err != nil {
		return nil, err
	}

	if len(attachmentIDs) == 0 {
		return nil, nil
	}

	var attachments []map[string]any
	err = c.execKW(ctx, "ir.attachment", "read", []any{attachmentIDs, []string{"name", "mimetype", "file_size"}}, nil, &attachments)
	if err != nil {
		return nil, err
	}

	result := make([]Attachment, len(attachments))
	for i, att := range attachments {
		result[i] = Attachment{
			ID:       int64(att["id"].(float64)),
			Name:     att["name"].(string),
			MimeType: att["mimetype"].(string),
			Size:     int64(att["file_size"].(float64)),
		}
	}

	return result, nil
}

// DownloadAttachment downloads an attachment's data
func (c *Client) DownloadAttachment(ctx context.Context, attachmentID int64) ([]byte, error) {
	var attachments []map[string]any
	err := c.execKW(ctx, "ir.attachment", "read", []any{[]int64{attachmentID}, []string{"datas"}}, nil, &attachments)
	if err != nil {
		return nil, err
	}

	if len(attachments) == 0 {
		return nil, fmt.Errorf("attachment not found")
	}

	encodedData, ok := attachments[0]["datas"].(string)
	if !ok {
		return nil, fmt.Errorf("no data in attachment")
	}

	return base64.StdEncoding.DecodeString(encodedData)
}

// ReopenTask moves a task from closed/done stage to an open stage
func (c *Client) ReopenTask(ctx context.Context, taskID int64, openStageID int64) (bool, error) {
	log.Debug().Int64("task_id", taskID).Int64("target_stage_id", openStageID).Msg("checking if task needs reopening")

	// First get current task to check if it needs reopening
	task, err := c.GetTask(ctx, taskID)
	if err != nil {
		return false, fmt.Errorf("failed to get task %d: %w", taskID, err)
	}

	log.Debug().Int64("task_id", taskID).Int64("current_stage_id", task.StageID).Str("stage_name", task.StageName).Msg("got task current state")

	// Check if task is already in an open stage
	if !c.IsTaskDone(task, nil) {
		log.Debug().Int64("task_id", taskID).Msg("task is already open, no reopening needed")
		return false, nil
	}

	log.Debug().Int64("task_id", taskID).Msg("task is closed, reopening")

	// Move task to open stage
	if err := c.SetTaskStage(ctx, taskID, openStageID); err != nil {
		return false, fmt.Errorf("failed to reopen task %d: %w", taskID, err)
	}

	log.Debug().Int64("task_id", taskID).Int64("new_stage_id", openStageID).Msg("task stage updated to open")

	// Add a comment about reopening using system message
	var msgResult any
	err = c.execKW(ctx, "project.task", "message_post", []any{taskID}, map[string]any{
		"body":         "🔄 Task byl automaticky znovu otevřen kvůli nové odpovědi zákazníka.",
		"message_type": "notification",
	}, &msgResult)
	if err != nil {
		log.Error().Err(err).Int64("task_id", taskID).Msg("failed to add reopening comment, but continuing")
		// Don't fail the whole operation if comment fails
	}

	return true, nil
}
