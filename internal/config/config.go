package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type App struct {
	PollSeconds     int      `yaml:"poll_seconds"`
	StatePath       string   `yaml:"state_path"`
	TicketPrefix    string   `yaml:"ticket_prefix"`
	DoneStageIDs    []int64  `yaml:"done_stage_ids"`
	ExcludedEmails  []string `yaml:"excluded_emails"`
	Operators       []string `yaml:"operators"`
	SLA             SLA      `yaml:"sla"`
	Debug           bool     `yaml:"debug"`
}

type SLA struct {
	StartTimeHours     int `yaml:"start_time_hours"`     // Hours to start working on task
	ResolutionTimeHours int `yaml:"resolution_time_hours"` // Hours to resolve task
}

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

type OdooStages struct {
	New        int64 `yaml:"new"`        // Nové
	Assigned   int64 `yaml:"assigned"`   // Přiřazeno
	InProgress int64 `yaml:"in_progress"` // Probíhá
	Done       int64 `yaml:"done"`       // Hotovo
}

type SlackCfg struct {
	WebhookURL string `yaml:"webhook_url"`
	BotToken   string `yaml:"bot_token"`
	ChannelID  string `yaml:"channel_id"`
}

type IMAPCfg struct {
	Host                string `yaml:"host"`
	Port                int    `yaml:"port"`
	Username            string `yaml:"username"`
	Password            string `yaml:"password"`
	Folder              string `yaml:"folder"`
	SearchTo            string `yaml:"search_to"`
	CustomProcessedFlag string `yaml:"custom_processed_flag"`
}

type SMTPCfg struct {
	Host           string `yaml:"host"`
	Port           int    `yaml:"port"`
	Username       string `yaml:"username"`
	Password       string `yaml:"password"`
	FromName       string `yaml:"from_name"`
	FromEmail      string `yaml:"from_email"`
	TimeoutSeconds int    `yaml:"timeout_seconds"`
}

type Config struct {
	App   App      `yaml:"app"`
	Odoo  Odoo     `yaml:"odoo"`
	Slack SlackCfg `yaml:"slack"`
	IMAP  IMAPCfg  `yaml:"imap"`
	SMTP  SMTPCfg  `yaml:"smtp"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
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

func (c *Config) TemplatesDirOrDefault() string { return "./templates" }
func (c *Config) OdooTimeout() time.Duration {
	return time.Duration(c.Odoo.TimeoutSeconds) * time.Second
}
