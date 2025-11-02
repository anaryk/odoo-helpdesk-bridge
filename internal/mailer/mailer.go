package mailer

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net/smtp"
	"time"

	"github.com/jordan-wright/email"
)

type SMTPConfig struct {
	Host      string
	Port      int
	Username  string
	Password  string
	FromName  string
	FromEmail string
	Timeout   time.Duration
}

type SMTPClient struct{ cfg SMTPConfig }

func NewSMTP(cfg SMTPConfig) *SMTPClient { return &SMTPClient{cfg: cfg} }

func (m *SMTPClient) Send(to, subject, body string) error {
	e := email.NewEmail()
	if m.cfg.FromName != "" {
		e.From = m.cfg.FromName + " <" + m.cfg.FromEmail + ">"
	} else {
		e.From = m.cfg.FromEmail
	}
	e.To = []string{to}
	e.Subject = subject
	e.Text = []byte(body)

	addr := m.cfg.Host + ":" + itoa(m.cfg.Port)

	var auth smtp.Auth
	if m.cfg.Username != "" || m.cfg.Password != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}

	// Use STARTTLS for port 587 (Gmail standard), direct TLS for port 465
	if m.cfg.Port == 587 {
		return e.SendWithStartTLS(addr, auth, &tls.Config{ServerName: m.cfg.Host})
	}
	return e.SendWithTLS(addr, auth, &tls.Config{ServerName: m.cfg.Host})
}

// Attachment represents an email attachment
type Attachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

// SendWithAttachments sends an email with attachments
func (m *SMTPClient) SendWithAttachments(to, subject, body string, attachments []Attachment) error {
	e := email.NewEmail()
	if m.cfg.FromName != "" {
		e.From = m.cfg.FromName + " <" + m.cfg.FromEmail + ">"
	} else {
		e.From = m.cfg.FromEmail
	}
	e.To = []string{to}
	e.Subject = subject
	e.Text = []byte(body)

	// Add attachments  
	for _, att := range attachments {
		_, err := e.Attach(bytes.NewReader(att.Data), att.Filename, att.ContentType)
		if err != nil {
			return fmt.Errorf("attach %s: %w", att.Filename, err)
		}
	}

	addr := m.cfg.Host + ":" + itoa(m.cfg.Port)

	var auth smtp.Auth
	if m.cfg.Username != "" || m.cfg.Password != "" {
		auth = smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)
	}

	// Use STARTTLS for port 587 (Gmail standard), direct TLS for port 465
	if m.cfg.Port == 587 {
		return e.SendWithStartTLS(addr, auth, &tls.Config{ServerName: m.cfg.Host})
	}
	return e.SendWithTLS(addr, auth, &tls.Config{ServerName: m.cfg.Host})
}

func itoa(v int) string { return fmt.Sprintf("%d", v) }
