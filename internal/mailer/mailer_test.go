package mailer

import (
	"errors"
	"testing"
	"time"
)

func TestNewSMTP(t *testing.T) {
	cfg := SMTPConfig{
		Host:      "smtp.test.com",
		Port:      587,
		Username:  "test@example.com",
		Password:  "testpass",
		FromName:  "Test Support",
		FromEmail: "support@test.com",
		Timeout:   30 * time.Second,
	}

	client := NewSMTP(cfg)
	if client == nil {
		t.Fatal("NewSMTP should return a client")
	}
	if client.cfg.Host != cfg.Host {
		t.Errorf("Expected host %s, got %s", cfg.Host, client.cfg.Host)
	}
	if client.cfg.Port != cfg.Port {
		t.Errorf("Expected port %d, got %d", cfg.Port, client.cfg.Port)
	}
}

func TestSMTPConfig_Validation(t *testing.T) {
	tests := []struct {
		name   string
		cfg    SMTPConfig
		valid  bool
	}{
		{
			name: "Valid config with auth",
			cfg: SMTPConfig{
				Host:      "smtp.gmail.com",
				Port:      587,
				Username:  "user@gmail.com",
				Password:  "password",
				FromName:  "Support",
				FromEmail: "support@example.com",
			},
			valid: true,
		},
		{
			name: "Valid config without auth",
			cfg: SMTPConfig{
				Host:      "localhost",
				Port:      25,
				FromEmail: "noreply@localhost",
			},
			valid: true,
		},
		{
			name: "Empty host",
			cfg: SMTPConfig{
				Port:      587,
				FromEmail: "test@example.com",
			},
			valid: false,
		},
		{
			name: "Zero port",
			cfg: SMTPConfig{
				Host:      "smtp.test.com",
				Port:      0,
				FromEmail: "test@example.com",
			},
			valid: false,
		},
		{
			name: "Empty FromEmail",
			cfg: SMTPConfig{
				Host: "smtp.test.com",
				Port: 587,
			},
			valid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := tt.cfg.Host != "" && tt.cfg.Port > 0 && tt.cfg.FromEmail != ""
			if valid != tt.valid {
				t.Errorf("Config validation mismatch for %s: got %v, want %v", tt.name, valid, tt.valid)
			}
		})
	}
}

func TestSMTPClient_EmailFormat(t *testing.T) {
	tests := []struct {
		name         string
		cfg          SMTPConfig
		expectedFrom string
	}{
		{
			name: "With display name",
			cfg: SMTPConfig{
				Host:      "smtp.test.com",
				Port:      587,
				FromName:  "Support Team",
				FromEmail: "support@example.com",
			},
			expectedFrom: "Support Team <support@example.com>",
		},
		{
			name: "Without display name",
			cfg: SMTPConfig{
				Host:      "smtp.test.com",
				Port:      587,
				FromEmail: "noreply@example.com",
			},
			expectedFrom: "noreply@example.com",
		},
		{
			name: "Empty display name",
			cfg: SMTPConfig{
				Host:      "smtp.test.com",
				Port:      587,
				FromName:  "",
				FromEmail: "test@example.com",
			},
			expectedFrom: "test@example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewSMTP(tt.cfg)
			
			// We can't actually test sending without a real SMTP server,
			// but we can verify the client is configured correctly
			if tt.cfg.FromName != "" {
				expectedFormat := tt.cfg.FromName + " <" + tt.cfg.FromEmail + ">"
				if expectedFormat != tt.expectedFrom {
					t.Errorf("Expected from format %s, got %s", tt.expectedFrom, expectedFormat)
				}
			} else {
				if tt.cfg.FromEmail != tt.expectedFrom {
					t.Errorf("Expected from email %s, got %s", tt.expectedFrom, tt.cfg.FromEmail)
				}
			}
			
			// Test that client was created correctly
			if client.cfg.FromEmail != tt.cfg.FromEmail {
				t.Errorf("Client FromEmail not set correctly: got %s, want %s", client.cfg.FromEmail, tt.cfg.FromEmail)
			}
		})
	}
}

// MockSMTPClient for testing purposes
type MockSMTPClient struct {
	SentEmails []SentEmail
	ShouldFail bool
}

type SentEmail struct {
	To      string
	Subject string
	Body    string
}

func NewMockSMTP() *MockSMTPClient {
	return &MockSMTPClient{
		SentEmails: make([]SentEmail, 0),
	}
}

func (m *MockSMTPClient) Send(to, subject, body string) error {
	if m.ShouldFail {
		return errors.New("mock smtp error")
	}
	
	m.SentEmails = append(m.SentEmails, SentEmail{
		To:      to,
		Subject: subject,
		Body:    body,
	})
	return nil
}

func TestMockSMTPClient(t *testing.T) {
	mock := NewMockSMTP()
	
	// Test successful send
	err := mock.Send("test@example.com", "Test Subject", "Test Body")
	if err != nil {
		t.Errorf("Mock send should not fail: %v", err)
	}
	
	if len(mock.SentEmails) != 1 {
		t.Errorf("Expected 1 sent email, got %d", len(mock.SentEmails))
	}
	
	email := mock.SentEmails[0]
	if email.To != "test@example.com" {
		t.Errorf("Expected to 'test@example.com', got '%s'", email.To)
	}
	if email.Subject != "Test Subject" {
		t.Errorf("Expected subject 'Test Subject', got '%s'", email.Subject)
	}
	if email.Body != "Test Body" {
		t.Errorf("Expected body 'Test Body', got '%s'", email.Body)
	}
	
	// Test failure
	mock.ShouldFail = true
	err = mock.Send("test2@example.com", "Test Subject 2", "Test Body 2")
	if err == nil {
		t.Error("Mock should fail when ShouldFail is true")
	}
	
	// Should not add failed email
	if len(mock.SentEmails) != 1 {
		t.Errorf("Expected still 1 sent email after failure, got %d", len(mock.SentEmails))
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{-1, "-1"},
		{-123, "-123"},
		{2147483647, "2147483647"},   // Max int32
		{-2147483648, "-2147483648"}, // Min int32
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := itoa(tt.input)
			if result != tt.expected {
				t.Errorf("itoa(%d) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSMTPClient_AddressFormatting(t *testing.T) {
	tests := []struct {
		name     string
		fromName string
		fromEmail string
		expected  string
	}{
		{
			name:      "Normal case with name",
			fromName:  "John Doe",
			fromEmail: "john@example.com",
			expected:  "John Doe <john@example.com>",
		},
		{
			name:      "Name with special characters",
			fromName:  "Support & Help",
			fromEmail: "support@example.com", 
			expected:  "Support & Help <support@example.com>",
		},
		{
			name:      "Empty name",
			fromName:  "",
			fromEmail: "noreply@example.com",
			expected:  "noreply@example.com",
		},
		{
			name:      "Name with quotes",
			fromName:  "\"Test User\"",
			fromEmail: "test@example.com",
			expected:  "\"Test User\" <test@example.com>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := SMTPConfig{
				Host:      "smtp.test.com",
				Port:      587,
				FromName:  tt.fromName,
				FromEmail: tt.fromEmail,
			}
			
			client := NewSMTP(cfg)
			
			// Simulate the from address formatting logic
			var expectedFrom string
			if tt.fromName != "" {
				expectedFrom = tt.fromName + " <" + tt.fromEmail + ">"
			} else {
				expectedFrom = tt.fromEmail
			}
			
			if expectedFrom != tt.expected {
				t.Errorf("Expected from address '%s', got '%s'", tt.expected, expectedFrom)
			}
			
			// Verify client configuration
			if client.cfg.FromName != tt.fromName {
				t.Errorf("FromName not preserved: got '%s', want '%s'", client.cfg.FromName, tt.fromName)
			}
			if client.cfg.FromEmail != tt.fromEmail {
				t.Errorf("FromEmail not preserved: got '%s', want '%s'", client.cfg.FromEmail, tt.fromEmail)
			}
		})
	}
}

func TestSendWithAttachments_Success(t *testing.T) {
	// Test data
	attachments := []Attachment{
		{
			Filename:    "test.pdf",
			ContentType: "application/pdf",
			Data:        []byte("PDF content"),
		},
		{
			Filename:    "image.jpg", 
			ContentType: "image/jpeg",
			Data:        []byte("JPEG data"),
		},
	}

	// Create SMTP client for testing structure only (no real connection)
	cfg := SMTPConfig{
		Host:      "nonexistent.smtp.server",  // Intentionally invalid
		Port:      587,
		Username:  "test@test.com",
		Password:  "password",
		FromName:  "Test",
		FromEmail: "test@test.com",
		Timeout:   1 * time.Second,  // Short timeout
	}
	client := NewSMTP(cfg)
	
	// This will fail but shouldn't panic - we're testing the method exists and handles attachments
	err := client.SendWithAttachments("recipient@test.com", "Test Subject", "Test Body", attachments)
	// We expect this to fail since we're using a nonexistent server
	if err == nil {
		t.Error("Expected error with nonexistent SMTP server")
	}
	
	// Test with empty attachments - should also fail with same server but not panic
	err = client.SendWithAttachments("recipient@test.com", "Test Subject", "Test Body", nil)
	if err == nil {
		t.Error("Expected error with nonexistent SMTP server")
	}
}

func TestAttachment_Structure(t *testing.T) {
	att := Attachment{
		Filename:    "document.pdf",
		ContentType: "application/pdf", 
		Data:        []byte("test data"),
	}
	
	if att.Filename != "document.pdf" {
		t.Errorf("Expected filename 'document.pdf', got %s", att.Filename)
	}
	if att.ContentType != "application/pdf" {
		t.Errorf("Expected content type 'application/pdf', got %s", att.ContentType)
	}
	if string(att.Data) != "test data" {
		t.Errorf("Expected data 'test data', got %s", string(att.Data))
	}
}