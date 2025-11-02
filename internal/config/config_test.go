package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfig_Load(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	// Create a test config file
	configContent := `
app:
  poll_seconds: 30
  state_path: "./test_state.db"
  ticket_prefix: "TEST"
  done_stage_ids: [1, 2, 3]
  debug: true
  excluded_emails:
    - "no-reply@test.com"
    - "noreply@test.com"
  operators:
    - "operator1@test.com"
    - "operator2@test.com"
  sla:
    start_time_hours: 2
    resolution_time_hours: 48

odoo:
  url: "https://test-odoo.example.com"
  db: "test_db"
  username: "test@example.com"
  password: "test_password"
  project_id: 456
  base_url: "https://test-odoo.example.com"
  timeout_seconds: 30
  stages:
    new: 10
    assigned: 11
    in_progress: 12
    done: 13

slack:
  webhook_url: "https://hooks.slack.com/services/TEST/TEST/TEST"
  bot_token: "xoxb-test-token"
  channel_id: "C1234TEST"

imap:
  host: "imap.test.com"
  port: 993
  username: "test@test.com"
  password: "test_password"
  folder: "INBOX"
  search_to: "test@test.com"
  custom_processed_flag: "X-TEST"

smtp:
  host: "smtp.test.com"
  port: 587
  username: "test@test.com"
  password: "test_password"
  from_name: "Test Support"
  from_email: "test@test.com"
  timeout_seconds: 30
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	// Load and test configuration
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test App config
	if cfg.App.PollSeconds != 30 {
		t.Errorf("Expected PollSeconds 30, got %d", cfg.App.PollSeconds)
	}
	if cfg.App.StatePath != "./test_state.db" {
		t.Errorf("Expected StatePath './test_state.db', got '%s'", cfg.App.StatePath)
	}
	if cfg.App.TicketPrefix != "TEST" {
		t.Errorf("Expected TicketPrefix 'TEST', got '%s'", cfg.App.TicketPrefix)
	}
	if len(cfg.App.DoneStageIDs) != 3 || cfg.App.DoneStageIDs[0] != 1 {
		t.Errorf("Expected DoneStageIDs [1,2,3], got %v", cfg.App.DoneStageIDs)
	}

	// Test ExcludedEmails
	if len(cfg.App.ExcludedEmails) != 2 {
		t.Errorf("Expected 2 excluded emails, got %d", len(cfg.App.ExcludedEmails))
	}
	if cfg.App.ExcludedEmails[0] != "no-reply@test.com" {
		t.Errorf("Expected first excluded email 'no-reply@test.com', got '%s'", cfg.App.ExcludedEmails[0])
	}

	// Test Operators
	if len(cfg.App.Operators) != 2 {
		t.Errorf("Expected 2 operators, got %d", len(cfg.App.Operators))
	}
	if cfg.App.Operators[0] != "operator1@test.com" {
		t.Errorf("Expected first operator 'operator1@test.com', got '%s'", cfg.App.Operators[0])
	}

	// Test Debug flag
	if !cfg.App.Debug {
		t.Errorf("Expected Debug true, got %v", cfg.App.Debug)
	}

	// Test SLA config
	if cfg.App.SLA.StartTimeHours != 2 {
		t.Errorf("Expected SLA StartTimeHours 2, got %d", cfg.App.SLA.StartTimeHours)
	}
	if cfg.App.SLA.ResolutionTimeHours != 48 {
		t.Errorf("Expected SLA ResolutionTimeHours 48, got %d", cfg.App.SLA.ResolutionTimeHours)
	}

	// Test Odoo config
	if cfg.Odoo.URL != "https://test-odoo.example.com" {
		t.Errorf("Expected Odoo URL 'https://test-odoo.example.com', got '%s'", cfg.Odoo.URL)
	}
	if cfg.Odoo.ProjectID != 456 {
		t.Errorf("Expected Odoo ProjectID 456, got %d", cfg.Odoo.ProjectID)
	}

	// Test Odoo Stages
	if cfg.Odoo.Stages.New != 10 {
		t.Errorf("Expected Odoo Stages.New 10, got %d", cfg.Odoo.Stages.New)
	}
	if cfg.Odoo.Stages.Assigned != 11 {
		t.Errorf("Expected Odoo Stages.Assigned 11, got %d", cfg.Odoo.Stages.Assigned)
	}
	if cfg.Odoo.Stages.InProgress != 12 {
		t.Errorf("Expected Odoo Stages.InProgress 12, got %d", cfg.Odoo.Stages.InProgress)
	}
	if cfg.Odoo.Stages.Done != 13 {
		t.Errorf("Expected Odoo Stages.Done 13, got %d", cfg.Odoo.Stages.Done)
	}

	// Test Slack config
	if cfg.Slack.BotToken != "xoxb-test-token" {
		t.Errorf("Expected Slack BotToken 'xoxb-test-token', got '%s'", cfg.Slack.BotToken)
	}
	if cfg.Slack.ChannelID != "C1234TEST" {
		t.Errorf("Expected Slack ChannelID 'C1234TEST', got '%s'", cfg.Slack.ChannelID)
	}

	// Test IMAP config
	if cfg.IMAP.Host != "imap.test.com" {
		t.Errorf("Expected IMAP Host 'imap.test.com', got '%s'", cfg.IMAP.Host)
	}
	if cfg.IMAP.Port != 993 {
		t.Errorf("Expected IMAP Port 993, got %d", cfg.IMAP.Port)
	}

	// Test SMTP config
	if cfg.SMTP.FromName != "Test Support" {
		t.Errorf("Expected SMTP FromName 'Test Support', got '%s'", cfg.SMTP.FromName)
	}
}

func TestConfig_Defaults(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "minimal_config.yaml")

	// Create minimal config file
	minimalContent := `
app: {}
odoo:
  url: "https://odoo.example.com"
  db: "odoo_db"
  username: "admin"
  password: "password"
  project_id: 1
slack: {}
imap:
  host: "imap.example.com"
smtp:
  host: "smtp.example.com"
`

	err := os.WriteFile(configPath, []byte(minimalContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create minimal config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test defaults
	if cfg.App.PollSeconds != 20 {
		t.Errorf("Expected default PollSeconds 20, got %d", cfg.App.PollSeconds)
	}
	if cfg.IMAP.Folder != "INBOX" {
		t.Errorf("Expected default IMAP Folder 'INBOX', got '%s'", cfg.IMAP.Folder)
	}
	if cfg.SMTP.TimeoutSeconds != 20 {
		t.Errorf("Expected default SMTP TimeoutSeconds 20, got %d", cfg.SMTP.TimeoutSeconds)
	}
	if cfg.Odoo.TimeoutSeconds != 20 {
		t.Errorf("Expected default Odoo TimeoutSeconds 20, got %d", cfg.Odoo.TimeoutSeconds)
	}
	if cfg.App.TicketPrefix != "TICKET" {
		t.Errorf("Expected default TicketPrefix 'TICKET', got '%s'", cfg.App.TicketPrefix)
	}
}

func TestConfig_OdooTimeout(t *testing.T) {
	cfg := &Config{
		Odoo: Odoo{
			TimeoutSeconds: 30,
		},
	}

	timeout := cfg.OdooTimeout()
	expected := 30 * time.Second
	if timeout != expected {
		t.Errorf("Expected timeout %v, got %v", expected, timeout)
	}
}

func TestConfig_TemplatesDirOrDefault(t *testing.T) {
	cfg := &Config{}
	
	dir := cfg.TemplatesDirOrDefault()
	expected := "./templates"
	if dir != expected {
		t.Errorf("Expected templates directory '%s', got '%s'", expected, dir)
	}
}

func TestConfig_LoadNonexistentFile(t *testing.T) {
	_, err := Load("/nonexistent/config.yaml")
	if err == nil {
		t.Error("Expected error when loading nonexistent config file")
	}
}

func TestConfig_LoadInvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid_config.yaml")

	// Create invalid YAML file
	invalidContent := `
app:
  poll_seconds: 
    - this is not valid
      - for poll_seconds
`

	err := os.WriteFile(configPath, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create invalid config file: %v", err)
	}

	_, err = Load(configPath)
	if err == nil {
		t.Error("Expected error when loading invalid YAML config file")
	}
}

func TestConfig_DebugFalse(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	// Create a test config file with debug: false
	configContent := `
app:
  poll_seconds: 30
  state_path: "./test_state.db"
  ticket_prefix: "TEST"
  debug: false
  excluded_emails: []
  operators: []
  sla:
    start_time_hours: 4
    resolution_time_hours: 24

odoo:
  url: "https://test-odoo.example.com"
  db: "test_db"
  username: "test@example.com"
  password: "test_password"
  project_id: 1
  base_url: "https://test-odoo.example.com"
  timeout_seconds: 30
  stages:
    new: 1
    assigned: 2
    in_progress: 3
    done: 4

slack:
  webhook_url: "https://hooks.slack.com/test"

imap:
  host: "imap.test.com"
  port: 993
  username: "test@test.com"
  password: "test_password"
  folder: "INBOX"

smtp:
  host: "smtp.test.com"
  port: 587
  username: "test@test.com"
  password: "test_password"
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test config file: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.App.Debug {
		t.Errorf("Expected Debug false, got %v", cfg.App.Debug)
	}
}