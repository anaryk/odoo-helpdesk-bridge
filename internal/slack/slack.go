package slack

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Config struct {
	WebhookURL string
	BotToken   string
	ChannelID  string
}

type Client struct {
	webhook   string
	botToken  string
	channelID string
	httpClient *http.Client
}

func New(url string) *Client { 
	return &Client{
		webhook: url,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	} 
}

func NewWithConfig(cfg Config) *Client {
	return &Client{
		webhook:   cfg.WebhookURL,
		botToken:  cfg.BotToken,
		channelID: cfg.ChannelID,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// SlackMessage represents a Slack message with timestamp for threading
type SlackMessage struct {
	Timestamp string `json:"ts"`
	Channel   string `json:"channel"`
}

// NotifyNewTask sends notification about new task and returns message info for threading
func (c *Client) NotifyNewTask(taskID int, title, url, body, assignedOperator string) (*SlackMessage, error) {
	// Use Bot API if available, otherwise fallback to webhook
	if c.botToken != "" && c.channelID != "" {
		return c.postNewTaskWithBot(taskID, title, url, body, assignedOperator)
	}
	
	// Fallback to webhook (no threading support)
	err := c.postNewTaskWebhook(taskID, title, url, body, assignedOperator)
	return nil, err
}

func (c *Client) postNewTaskWithBot(taskID int, title, url, body, assignedOperator string) (*SlackMessage, error) {
	// Truncate body to first 300 chars for readability
	truncatedBody := body
	if len(body) > 300 {
		truncatedBody = body[:300] + "..."
	}
	
	// Format timestamp
	timestamp := time.Now().Format("02.01.2006 15:04")
	
	blocks := []any{
		section("<!channel> :rotating_light: *Nový support task*"),
		section("*Task:* " + title),
		section("*ID:* " + itoa(taskID)),
		section("*Obsah:* " + truncatedBody),
		section("*Přiřazeno:* " + assignedOperator),
		section("*Vytvořeno:* " + timestamp),
		section("<" + url + "|:point_right: Otevřít v Odoo>"),
	}
	
	payload := map[string]any{
		"channel": c.channelID,
		"text":    "<!channel> :rotating_light: *Nový support task*",
		"blocks":  blocks,
	}
	
	return c.callSlackAPI("chat.postMessage", payload)
}

func (c *Client) postNewTaskWebhook(taskID int, title, url, body, assignedOperator string) error {
	if c.webhook == "" {
		return nil
	}
	
	// Truncate body to first 300 chars for readability
	truncatedBody := body
	if len(body) > 300 {
		truncatedBody = body[:300] + "..."
	}
	
	// Format timestamp
	timestamp := time.Now().Format("02.01.2006 15:04")
	
	payload := map[string]any{
		"text": "<!channel> :rotating_light: *Nový support task*",
		"blocks": []any{
			section("<!channel> :rotating_light: *Nový support task*"),
			section("*Task:* " + title),
			section("*ID:* " + itoa(taskID)),
			section("*Obsah:* " + truncatedBody),
			section("*Přiřazeno:* " + assignedOperator),
			section("*Vytvořeno:* " + timestamp),
			section("<" + url + "|:point_right: Otevřít v Odoo>"),
		},
	}
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", c.webhook, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// NotifyTaskAssigned posts to thread that task was assigned
func (c *Client) NotifyTaskAssigned(parentMsg *SlackMessage, taskID int, assigneeName string) error {
	if c.botToken == "" || c.channelID == "" || parentMsg == nil {
		return nil
	}
	
	payload := map[string]any{
		"channel":    c.channelID,  // Use configured channel ID instead of stored one
		"thread_ts":  parentMsg.Timestamp,
		"text":       fmt.Sprintf(":male-technologist: Task #%d byl automaticky přiřazen operátorovi *%s*", taskID, assigneeName),
	}
	
	_, err := c.callSlackAPI("chat.postMessage", payload)
	return err
}

// NotifyTaskCompleted posts to thread that task was completed
func (c *Client) NotifyTaskCompleted(parentMsg *SlackMessage, taskID int, title string) error {
	if c.botToken == "" || c.channelID == "" || parentMsg == nil {
		return nil
	}
	
	payload := map[string]any{
		"channel":    c.channelID,  // Use configured channel ID instead of stored one
		"thread_ts":  parentMsg.Timestamp,
		"text":       fmt.Sprintf(":heavy_check_mark: Task #%d *%s* byl úspěšně dokončen a uzavřen", taskID, title),
	}
	
	_, err := c.callSlackAPI("chat.postMessage", payload)
	return err
}

// NotifyTaskReopened posts to thread that task was reopened
func (c *Client) NotifyTaskReopened(parentMsg *SlackMessage, taskID int, title string, assigneeName string) error {
	if c.botToken == "" || c.channelID == "" || parentMsg == nil {
		return nil
	}
	
	var text string
	if assigneeName != "" {
		text = fmt.Sprintf(":arrows_counterclockwise: Task #%d *%s* byl znovu otevřen zákazníkem a přiřazen operátorovi *%s*", taskID, title, assigneeName)
	} else {
		text = fmt.Sprintf(":arrows_counterclockwise: Task #%d *%s* byl znovu otevřen zákazníkem", taskID, title)
	}
	
	payload := map[string]any{
		"channel":    c.channelID,
		"thread_ts":  parentMsg.Timestamp,
		"text":       text,
	}
	
	_, err := c.callSlackAPI("chat.postMessage", payload)
	return err
}

// UpdateTaskStatusCompleted updates the original message to show completed status
func (c *Client) UpdateTaskStatusCompleted(parentMsg *SlackMessage, taskID int, title, url, assignedOperator string) error {
	if c.botToken == "" || c.channelID == "" || parentMsg == nil {
		return nil
	}
	
	// Update original message with completed status
	text := fmt.Sprintf(":heavy_check_mark: *Dokončeno* | Task #%d: *%s*\n:link: <%s|Otevřít v Odoo>\n:male-technologist: Operátor: *%s*", taskID, title, url, assignedOperator)
	
	payload := map[string]any{
		"channel": c.channelID,
		"ts":      parentMsg.Timestamp,
		"text":    text,
	}
	
	_, err := c.callSlackAPI("chat.update", payload)
	return err
}

// UpdateTaskStatusReopened updates the original message to show reopened status
func (c *Client) UpdateTaskStatusReopened(parentMsg *SlackMessage, taskID int, title, url, assignedOperator string) error {
	if c.botToken == "" || c.channelID == "" || parentMsg == nil {
		return nil
	}
	
	// Update original message with reopened status
	text := fmt.Sprintf(":warning: *Znovu otevřeno* | Task #%d: *%s*\n:link: <%s|Otevřít v Odoo>\n:male-technologist: Operátor: *%s*", taskID, title, url, assignedOperator)
	
	payload := map[string]any{
		"channel": c.channelID,
		"ts":      parentMsg.Timestamp,
		"text":    text,
	}
	
	_, err := c.callSlackAPI("chat.update", payload)
	return err
}

// NotifySLAViolation posts to thread about SLA violation and mentions channel
func (c *Client) NotifySLAViolation(parentMsg *SlackMessage, taskID int, title string, violationType string) error {
	if c.botToken == "" || c.channelID == "" || parentMsg == nil {
		return nil
	}
	
	var message string
	switch violationType {
	case "start_time":
		message = fmt.Sprintf("<!channel> :warning: *SLA PORUŠENÍ* - Task #%d *%s* nebyl zahájen včas!", taskID, title)
	case "resolution_time":
		message = fmt.Sprintf("<!channel> :warning: *SLA PORUŠENÍ* - Task #%d *%s* nebyl vyřešen včas!", taskID, title)
	default:
		message = fmt.Sprintf("<!channel> :warning: *SLA PORUŠENÍ* - Task #%d *%s*", taskID, title)
	}
	
	payload := map[string]any{
		"channel":    c.channelID,  // Use configured channel ID instead of stored one
		"thread_ts":  parentMsg.Timestamp,
		"text":       message,
	}
	
	_, err := c.callSlackAPI("chat.postMessage", payload)
	return err
}

func (c *Client) callSlackAPI(method string, payload map[string]any) (*SlackMessage, error) {
	b, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://slack.com/api/"+method, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.botToken)
	
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	var result struct {
		OK      bool         `json:"ok"`
		Error   string       `json:"error,omitempty"`
		Message SlackMessage `json:"message,omitempty"`
	}
	
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	
	if !result.OK {
		return nil, fmt.Errorf("slack API error: %s", result.Error)
	}
	
	return &result.Message, nil
}

func section(t string) map[string]any {
	return map[string]any{"type": "section", "text": map[string]string{"type": "mrkdwn", "text": t}}
}
func itoa(v int) string { return fmt.Sprintf("%d", v) }
