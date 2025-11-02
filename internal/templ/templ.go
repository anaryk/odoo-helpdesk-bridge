// Package templ provides email template rendering functionality.
package templ

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

// Engine provides template rendering capabilities for email messages.
type Engine struct{ dir string }

// New creates a new template engine with the specified templates directory.
func New(dir string) (*Engine, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	return &Engine{dir: dir}, nil
}

func (e *Engine) render(file string, data any) (string, error) {
	// Validate filename to prevent directory traversal
	if strings.Contains(file, "..") || strings.Contains(file, "/") {
		return "", fmt.Errorf("invalid template file: %s", file)
	}

	path := filepath.Join(e.dir, file)
	b, err := os.ReadFile(path) // #nosec G304 - file is validated above
	if err != nil {
		return "", err
	}
	tpl, err := template.New(filepath.Base(file)).Parse(string(b))
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, data); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// RenderNewTicket renders email templates for new ticket notifications.
func (e *Engine) RenderNewTicket(prefix string, taskID int, customerName, originalBody string, slaStartHours, slaResolutionHours int) (string, string, error) {
	subj, err := e.render("new_ticket_subject.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID,
	})
	if err != nil {
		return "", "", err
	}
	body, err := e.render("new_ticket_body.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "CustomerName": customerName, "OriginalBody": originalBody,
		"SLAStartHours": slaStartHours, "SLAResolutionHours": slaResolutionHours,
	})
	return strings.TrimSpace(subj), body, err
}

// RenderAgentReply renders email templates for agent reply notifications.
func (e *Engine) RenderAgentReply(prefix string, taskID int, subject, customerName, agentMsg string) (string, string, error) {
	subj, err := e.render("agent_reply_subject.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "Subject": subject,
	})
	if err != nil {
		return "", "", err
	}
	body, err := e.render("agent_reply_body.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "CustomerName": customerName, "AgentMessage": agentMsg,
	})
	return strings.TrimSpace(subj), body, err
}

// RenderTicketClosed renders email templates for ticket closure notifications.
func (e *Engine) RenderTicketClosed(prefix string, taskID int, taskURL, customerName string) (string, string, error) {
	subj, err := e.render("ticket_closed_subject.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID,
	})
	if err != nil {
		return "", "", err
	}
	body, err := e.render("ticket_closed_body.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "TaskURL": taskURL, "CustomerName": customerName,
	})
	return strings.TrimSpace(subj), body, err
}
