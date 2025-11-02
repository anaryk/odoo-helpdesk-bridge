// Package config provides configuration management for the odoo-helpdesk-bridge application.
package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// App holds application-specific configuration settings.
type App struct {
	PollSeconds    int      `yaml:"poll_seconds"`
	StatePath      string   `yaml:"state_path"`
	TicketPrefix   string   `yaml:"ticket_prefix"`
	DoneStageIDs   []int64  `yaml:"done_stage_ids"`
	ExcludedEmails []string `yaml:"excluded_emails"`
	Operators      []string `yaml:"operators"`
	SLA            SLA      `yaml:"sla"`
	Debug          bool     `yaml:"debug"`
}

// SLA holds Service Level Agreement configuration settings.
type SLA struct {
	StartTimeHours      int `yaml:"start_time_hours"`      // Hours to start working on task
	ResolutionTimeHours int `yaml:"resolution_time_hours"` // Hours to resolve task
}

// Odoo holds Odoo ERP system configuration settings.
type Odoo struct {
	URL            string     `yaml:"url"`
	DB             string     `yaml:"db"`
	Username       string     `yaml:"username"`
	Password       string     `yaml:"password"`
	ProjectID      int        `yaml:"project_id"`
	BaseURL        string     `yaml:"base_url"`
	TimeoutSeconds int        `yaml:"timeout_seconds"`
	Stages         OdooStages `yaml:"stages"`
}

// OdooStages defines the stage IDs used in Odoo project management.
type OdooStages struct {
	New        int64 `yaml:"new"`         // Nové
	Assigned   int64 `yaml:"assigned"`    // Přiřazeno
	InProgress int64 `yaml:"in_progress"` // Probíhá
	Done       int64 `yaml:"done"`        // Hotovo
}

// SlackCfg holds Slack integration configuration settings.
type SlackCfg struct {
	WebhookURL string `yaml:"webhook_url"`
	BotToken   string `yaml:"bot_token"`
	ChannelID  string `yaml:"channel_id"`
}

// IMAPCfg holds IMAP email server configuration settings.
type IMAPCfg struct {
	Host                string `yaml:"host"`
	Port                int    `yaml:"port"`
	Username            string `yaml:"username"`
	Password            string `yaml:"password"`
	Folder              string `yaml:"folder"`
	SearchTo            string `yaml:"search_to"`
	CustomProcessedFlag string `yaml:"custom_processed_flag"`
}

// SMTPCfg holds SMTP email server configuration settings.
type SMTPCfg struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	FromName       string `yaml:"from_name"`
	FromEmail      string `yaml:"from_email"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

// Config holds the complete application configuration.
type Config struct {
	App   App      `yaml:"app"`
	Odoo  Odoo     `yaml:"odoo"`
	Slack SlackCfg `yaml:"slack"`
	IMAP  IMAPCfg  `yaml:"imap"`
	SMTP  SMTPCfg  `yaml:"smtp"`
}

// Load reads and parses configuration from a YAML file.
func Load(path string) (*Config, error) {
	// Validate path to prevent directory traversal
	if strings.Contains(path, "..") {
		return nil, fmt.Errorf("invalid path: contains directory traversal")
	}

	b, err := os.ReadFile(path) // #nosec G304 - path is validated above
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if c.App.PollSeconds == 0 {
		c.App.PollSeconds = 20
	}
	if c.IMAP.Folder == "" {
		c.IMAP.Folder = "INBOX"
	}
	if c.SMTP.TimeoutSeconds == 0 {
		c.SMTP.TimeoutSeconds = 20
	}
	if c.Odoo.TimeoutSeconds == 0 {
		c.Odoo.TimeoutSeconds = 20
	}
	if c.App.TicketPrefix == "" {
		c.App.TicketPrefix = "TICKET"
	}
	return &c, nil
}

// TemplatesDirOrDefault returns the default templates directory path.
func (c *Config) TemplatesDirOrDefault() string { return "./templates" }

// OdooTimeout returns the configured Odoo API timeout as a time.Duration.
func (c *Config) OdooTimeout() time.Duration {
	return time.Duration(c.Odoo.TimeoutSeconds) * time.Second
}
