package templ

import (
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

type Engine struct{ dir string }

func New(dir string) (*Engine, error) {
	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}
	return &Engine{dir: dir}, nil
}

func (e *Engine) render(file string, data any) (string, string, error) {
	path := filepath.Join(e.dir, file)
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	tpl, err := template.New(filepath.Base(file)).Parse(string(b))
	if err != nil {
		return "", "", err
	}
	var sb strings.Builder
	if err := tpl.Execute(&sb, data); err != nil {
		return "", "", err
	}
	return "", sb.String(), nil
}

func (e *Engine) RenderNewTicket(prefix string, taskID int, customerName, originalBody string, slaStartHours, slaResolutionHours int) (string, string, error) {
	_, subj, err := e.render("new_ticket_subject.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID,
	})
	if err != nil {
		return "", "", err
	}
	_, body, err := e.render("new_ticket_body.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "CustomerName": customerName, "OriginalBody": originalBody,
		"SLAStartHours": slaStartHours, "SLAResolutionHours": slaResolutionHours,
	})
	return strings.TrimSpace(subj), body, err
}

func (e *Engine) RenderAgentReply(prefix string, taskID int, subject, customerName, agentMsg string) (string, string, error) {
	_, subj, err := e.render("agent_reply_subject.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "Subject": subject,
	})
	if err != nil {
		return "", "", err
	}
	_, body, err := e.render("agent_reply_body.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "CustomerName": customerName, "AgentMessage": agentMsg,
	})
	return strings.TrimSpace(subj), body, err
}

func (e *Engine) RenderTicketClosed(prefix string, taskID int, taskURL, customerName string) (string, string, error) {
	_, subj, err := e.render("ticket_closed_subject.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID,
	})
	if err != nil {
		return "", "", err
	}
	_, body, err := e.render("ticket_closed_body.tmpl", map[string]any{
		"TicketPrefix": prefix, "TaskID": taskID, "TaskURL": taskURL, "CustomerName": customerName,
	})
	return strings.TrimSpace(subj), body, err
}
