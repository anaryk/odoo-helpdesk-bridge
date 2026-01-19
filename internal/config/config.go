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
	NoReplyEmails  []string `yaml:"no_reply_emails"` // Emails that create tickets but don't receive any responses
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

	// Set defaults
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

	// Set SLA defaults
	if c.App.SLA.StartTimeHours == 0 {
		c.App.SLA.StartTimeHours = 4
	}
	if c.App.SLA.ResolutionTimeHours == 0 {
		c.App.SLA.ResolutionTimeHours = 24
	}

	// Validate required fields
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return &c, nil
}

// Validate checks that all required configuration fields are set.
func (c *Config) Validate() error {
	var errors []string

	// Odoo validation
	if c.Odoo.URL == "" {
		errors = append(errors, "odoo.url is required")
	}
	if c.Odoo.DB == "" {
		errors = append(errors, "odoo.db is required")
	}
	if c.Odoo.Username == "" {
		errors = append(errors, "odoo.username is required")
	}
	if c.Odoo.Password == "" {
		errors = append(errors, "odoo.password is required")
	}
	if c.Odoo.ProjectID == 0 {
		errors = append(errors, "odoo.project_id is required")
	}

	// Stage IDs validation (critical for SLA)
	if c.Odoo.Stages.New == 0 {
		errors = append(errors, "odoo.stages.new is required for SLA tracking")
	}

	// IMAP validation
	if c.IMAP.Host == "" {
		errors = append(errors, "imap.host is required")
	}
	if c.IMAP.Username == "" {
		errors = append(errors, "imap.username is required")
	}
	if c.IMAP.Password == "" {
		errors = append(errors, "imap.password is required")
	}

	// SMTP validation
	if c.SMTP.Host == "" {
		errors = append(errors, "smtp.host is required")
	}
	if c.SMTP.FromEmail == "" {
		errors = append(errors, "smtp.from_email is required")
	}

	if len(errors) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(errors, ", "))
	}

	return nil
}

// TemplatesDirOrDefault returns the default templates directory path.
func (c *Config) TemplatesDirOrDefault() string { return "./templates" }

// OdooTimeout returns the configured Odoo API timeout as a time.Duration.
func (c *Config) OdooTimeout() time.Duration {
	return time.Duration(c.Odoo.TimeoutSeconds) * time.Second
}
