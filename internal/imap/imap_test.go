package imap

import (
	"strings"
	"testing"
)

func TestExtractTicketID(t *testing.T) {
	tests := []struct {
		name           string
		subject        string
		expectedPrefix string
		expectedID     int
		expectedOK     bool
	}{
		{
			name:           "Valid ticket ID",
			subject:        "Re: [ML-#123] Problem with system",
			expectedPrefix: "ML",
			expectedID:     123,
			expectedOK:     true,
		},
		{
			name:           "Valid ticket ID with spaces",
			subject:        "Re: [ ML - #456 ] Another issue",
			expectedPrefix: "ML",
			expectedID:     456,
			expectedOK:     true,
		},
		{
			name:           "Case insensitive prefix",
			subject:        "Re: [ml-#789] Case test",
			expectedPrefix: "ML",
			expectedID:     789,
			expectedOK:     true,
		},
		{
			name:           "Wrong prefix",
			subject:        "Re: [XX-#123] Wrong prefix",
			expectedPrefix: "ML",
			expectedID:     0,
			expectedOK:     false,
		},
		{
			name:           "No ticket ID",
			subject:        "Simple subject without ID",
			expectedPrefix: "ML",
			expectedID:     0,
			expectedOK:     false,
		},
		{
			name:           "Invalid format",
			subject:        "Re: [ML-456] Missing hash",
			expectedPrefix: "ML",
			expectedID:     0,
			expectedOK:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := ExtractTicketID(tt.subject, tt.expectedPrefix)
			if id != tt.expectedID {
				t.Errorf("ExtractTicketID() id = %v, want %v", id, tt.expectedID)
			}
			if ok != tt.expectedOK {
				t.Errorf("ExtractTicketID() ok = %v, want %v", ok, tt.expectedOK)
			}
		})
	}
}

func TestCleanBody(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "Remove quoted text",
			input: `This is my reply.

> Previous message content
> More quoted text`,
			expected: "This is my reply.",
		},
		{
			name: "Remove original message separator",
			input: `My response here.

--- Original Message ---
From: someone@example.com
Subject: Test`,
			expected: "My response here.",
		},
		{
			name:     "Clean text without quotes",
			input:    "Just a simple message without quotes.",
			expected: "Just a simple message without quotes.",
		},
		{
			name:     "Empty input",
			input:    "",
			expected: "",
		},
		{
			name: "Only quoted text",
			input: `> All quoted
> Nothing original`,
			expected: `> All quoted
> Nothing original`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CleanBody(tt.input)
			result = strings.TrimSpace(result)
			if result != tt.expected {
				t.Errorf("CleanBody() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestHtmlToText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "Simple HTML",
			input:    "<p>Hello <b>world</b></p>",
			expected: "Hello world",
		},
		{
			name:     "HTML with line breaks",
			input:    "<p>First line</p><br/><p>Second line</p>",
			expected: "First line\n\nSecond line",
		},
		{
			name:     "Plain text",
			input:    "Just plain text",
			expected: "Just plain text",
		},
		{
			name:     "Empty input",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := htmlToText(tt.input)
			if result != tt.expected {
				t.Errorf("htmlToText() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestParseEmailContent_WithAttachments(t *testing.T) {
	// Mock MIME email with attachment
	mimeContent := `Content-Type: multipart/mixed; boundary="boundary123"

--boundary123
Content-Type: text/plain

This is the email body.

--boundary123
Content-Type: image/jpeg; name="test.jpg"
Content-Disposition: attachment; filename="test.jpg"
Content-Transfer-Encoding: base64

/9j/4AAQSkZJRgABAQEAYABgAAD/2wBDAAEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQH/2wBDAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQH/wAARCAACAAIDASIAAhEBAxEB/8QAFQABAQAAAAAAAAAAAAAAAAAAAAv/xAAUEAEAAAAAAAAAAAAAAAAAAAAA/8QAFQEBAQAAAAAAAAAAAAAAAAAAAAX/xAAUEQEAAAAAAAAAAAAAAAAAAAAA/9oADAMBAAIRAxEAPwA/AB8=

--boundary123--`

	body, attachments := parseEmailContent(strings.NewReader(mimeContent))

	expectedBody := "This is the email body."
	if strings.TrimSpace(body) != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, strings.TrimSpace(body))
	}

	if len(attachments) != 1 {
		t.Errorf("Expected 1 attachment, got %d", len(attachments))
		return
	}

	att := attachments[0]
	if att.Filename != "test.jpg" {
		t.Errorf("Expected filename 'test.jpg', got %q", att.Filename)
	}
	if att.ContentType != "image/jpeg" {
		t.Errorf("Expected content type 'image/jpeg', got %q", att.ContentType)
	}
	if att.Size == 0 {
		t.Errorf("Expected attachment to have size > 0")
	}
}

func TestParseEmailContent_NestedMultipart(t *testing.T) {
	mimeContent := `Content-Type: multipart/related; boundary="boundary123"

--boundary123
Content-Type: multipart/alternative; boundary="boundary456"

--boundary456
Content-Type: text/plain; charset="UTF-8"

Tohle je text emailu s problémem.

--boundary456
Content-Type: text/html; charset="UTF-8"

<html><body><p>Tohle je text emailu s problémem.</p></body></html>

--boundary456--

--boundary123
Content-Type: image/jpeg
Content-Disposition: inline; filename="test.jpg"
Content-Transfer-Encoding: base64

/9j/4AAQSkZJRgABAQEAYABgAAD/2Q==

--boundary123--`

	body, attachments := parseEmailContent(strings.NewReader(mimeContent))

	expectedBody := "Tohle je text emailu s problémem."
	if strings.TrimSpace(body) != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, strings.TrimSpace(body))
	}

	if len(attachments) != 1 {
		t.Errorf("Expected 1 attachment, got %d", len(attachments))
		return
	}
}

func TestParseEmailContent_PlainText(t *testing.T) {
	// Create proper email format for plain text
	plainContent := `Content-Type: text/plain

Just plain text email without attachments`

	body, attachments := parseEmailContent(strings.NewReader(plainContent))

	expectedBody := "Just plain text email without attachments"
	if strings.TrimSpace(body) != expectedBody {
		t.Errorf("Expected body %q, got %q", expectedBody, strings.TrimSpace(body))
	}

	if len(attachments) != 0 {
		t.Errorf("Expected 0 attachments, got %d", len(attachments))
	}
}

func TestGetExtensionForContentType(t *testing.T) {
	tests := []struct {
		contentType   string
		expectedOneOf []string // Multiple acceptable extensions
	}{
		{"image/jpeg", []string{".jpeg", ".jpg", ".jpe"}}, // Different systems may return different extensions
		{"image/png", []string{".png"}},
		{"application/pdf", []string{".pdf"}},
		{"application/vnd.openxmlformats-officedocument.wordprocessingml.document", []string{".docx"}},
		{"unknown/type", []string{""}},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := getExtensionForContentType(tt.contentType)

			// Check if result is one of the expected values
			found := false
			for _, expected := range tt.expectedOneOf {
				if result == expected {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("getExtensionForContentType(%q) = %q, want one of %v", tt.contentType, result, tt.expectedOneOf)
			}
		})
	}
}
