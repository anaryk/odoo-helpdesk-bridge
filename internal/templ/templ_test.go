package templ

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	// Create temporary directory with templates
	tmpDir := t.TempDir()

	// Test with valid directory
	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("New() with valid dir should not fail: %v", err)
	}
	if engine == nil {
		t.Fatal("New() should return an engine")
	}
	if engine.dir != tmpDir {
		t.Errorf("Expected dir %s, got %s", tmpDir, engine.dir)
	}

	// Test with non-existent directory
	_, err = New("/nonexistent/directory")
	if err == nil {
		t.Error("New() with non-existent dir should fail")
	}
}

func TestEngine_render(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test template file
	templateContent := "Hello {{.Name}}! Your ID is {{.ID}}."
	templatePath := filepath.Join(tmpDir, "test.tmpl")
	err := os.WriteFile(templatePath, []byte(templateContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create test template: %v", err)
	}

	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test successful rendering
	data := map[string]any{
		"Name": "John Doe",
		"ID":   123,
	}

	// render() returns (string, string, error) - first string is empty, second contains result
	_, result, err := engine.render("test.tmpl", data)
	if err != nil {
		t.Fatalf("render() should not fail: %v", err)
	}

	expected := "Hello John Doe! Your ID is 123."
	if result != expected {
		t.Errorf("Expected '%s', got '%s'", expected, result)
	}

	// Test with missing template file
	_, _, err = engine.render("nonexistent.tmpl", data)
	if err == nil {
		t.Error("render() with missing file should fail")
	}
}

func TestEngine_render_InvalidTemplate(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid template file
	invalidTemplate := "Hello {{.Name}! Missing closing brace"
	templatePath := filepath.Join(tmpDir, "invalid.tmpl")
	err := os.WriteFile(templatePath, []byte(invalidTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create invalid template: %v", err)
	}

	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test with invalid template syntax
	_, _, err = engine.render("invalid.tmpl", map[string]any{"Name": "Test"})
	if err == nil {
		t.Error("render() with invalid template should fail")
	}
}

func TestEngine_RenderNewTicket(t *testing.T) {
	tmpDir := t.TempDir()

	// Create template files
	subjectTemplate := "[{{.TicketPrefix}}-#{{.TaskID}}] New support ticket"
	bodyTemplate := `Hello {{.CustomerName}},

We received your request and created ticket {{.TicketPrefix}}-#{{.TaskID}}.

📋 Processing information:
• We usually start working within {{.SLAStartHours}} hours
• On average we resolve requests within {{.SLAResolutionHours}} hours
• For additional questions, reply to this email

Original message:
{{.OriginalBody}}

Best regards,
Support Team`

	err := os.WriteFile(filepath.Join(tmpDir, "new_ticket_subject.tmpl"), []byte(subjectTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create subject template: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "new_ticket_body.tmpl"), []byte(bodyTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create body template: %v", err)
	}

	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test rendering
	subject, body, err := engine.RenderNewTicket("ML", 123, "John Doe", "Help me please!", 4, 24)
	if err != nil {
		t.Fatalf("RenderNewTicket() should not fail: %v", err)
	}

	expectedSubject := "[ML-#123] New support ticket"
	if subject != expectedSubject {
		t.Errorf("Expected subject '%s', got '%s'", expectedSubject, subject)
	}

	if !strings.Contains(body, "Hello John Doe") {
		t.Error("Body should contain customer name")
	}
	if !strings.Contains(body, "ML-#123") {
		t.Error("Body should contain ticket ID")
	}
	if !strings.Contains(body, "usually start working within 4 hours") {
		t.Errorf("Body should contain average start time. Body:\n%s", body)
	}
	if !strings.Contains(body, "On average we resolve requests within 24 hours") {
		t.Errorf("Body should contain average resolution time. Body:\n%s", body)
	}
	if !strings.Contains(body, "Help me please!") {
		t.Error("Body should contain original message")
	}
}

func TestEngine_RenderAgentReply(t *testing.T) {
	tmpDir := t.TempDir()

	// Create template files
	subjectTemplate := "Re: [{{.TicketPrefix}}-#{{.TaskID}}] {{.Subject}}"
	bodyTemplate := `Hello {{.CustomerName}},

{{.AgentMessage}}

📋 For additional questions, reply to this email - it will automatically be assigned to your request.

Best regards,
Support Team`

	err := os.WriteFile(filepath.Join(tmpDir, "agent_reply_subject.tmpl"), []byte(subjectTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create subject template: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "agent_reply_body.tmpl"), []byte(bodyTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create body template: %v", err)
	}

	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test rendering
	subject, body, err := engine.RenderAgentReply("ML", 456, "Original Subject", "Jane Smith", "We fixed the issue.")
	if err != nil {
		t.Fatalf("RenderAgentReply() should not fail: %v", err)
	}

	expectedSubject := "Re: [ML-#456] Original Subject"
	if subject != expectedSubject {
		t.Errorf("Expected subject '%s', got '%s'", expectedSubject, subject)
	}

	if !strings.Contains(body, "Hello Jane Smith") {
		t.Error("Body should contain customer name")
	}
	if !strings.Contains(body, "We fixed the issue.") {
		t.Error("Body should contain agent message")
	}
	if !strings.Contains(body, "reply to this email - it will automatically") {
		t.Errorf("Body should contain reply instructions. Body:\n%s", body)
	}
}

func TestEngine_RenderTicketClosed(t *testing.T) {
	tmpDir := t.TempDir()

	// Create template files
	subjectTemplate := "[{{.TicketPrefix}}-#{{.TaskID}}] Ticket resolved"
	bodyTemplate := `Hello {{.CustomerName}},

Your ticket {{.TicketPrefix}}-#{{.TaskID}} has been resolved.

View ticket: {{.TaskURL}}

Thank you for using our support!

Best regards,
Support Team`

	err := os.WriteFile(filepath.Join(tmpDir, "ticket_closed_subject.tmpl"), []byte(subjectTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create subject template: %v", err)
	}

	err = os.WriteFile(filepath.Join(tmpDir, "ticket_closed_body.tmpl"), []byte(bodyTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create body template: %v", err)
	}

	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test rendering
	subject, body, err := engine.RenderTicketClosed("HELP", 789, "http://example.com/task/789", "Bob Wilson")
	if err != nil {
		t.Fatalf("RenderTicketClosed() should not fail: %v", err)
	}

	expectedSubject := "[HELP-#789] Ticket resolved"
	if subject != expectedSubject {
		t.Errorf("Expected subject '%s', got '%s'", expectedSubject, subject)
	}

	if !strings.Contains(body, "Hello Bob Wilson") {
		t.Error("Body should contain customer name")
	}
	if !strings.Contains(body, "HELP-#789") {
		t.Error("Body should contain ticket ID")
	}
	if !strings.Contains(body, "http://example.com/task/789") {
		t.Error("Body should contain task URL")
	}
	if !strings.Contains(body, "has been resolved") {
		t.Error("Body should contain resolution message")
	}
}

func TestEngine_MissingTemplates(t *testing.T) {
	tmpDir := t.TempDir()

	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test with missing templates
	_, _, err = engine.RenderNewTicket("ML", 123, "John", "message", 4, 24)
	if err == nil {
		t.Error("RenderNewTicket() should fail with missing templates")
	}

	_, _, err = engine.RenderAgentReply("ML", 123, "subject", "John", "message")
	if err == nil {
		t.Error("RenderAgentReply() should fail with missing templates")
	}

	_, _, err = engine.RenderTicketClosed("ML", 123, "http://example.com", "John")
	if err == nil {
		t.Error("RenderTicketClosed() should fail with missing templates")
	}
}

func TestEngine_EmptyData(t *testing.T) {
	tmpDir := t.TempDir()

	// Create simple template
	templateContent := "{{.Value}}"
	err := os.WriteFile(filepath.Join(tmpDir, "simple.tmpl"), []byte(templateContent), 0644)
	if err != nil {
		t.Fatalf("Failed to create template: %v", err)
	}

	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}

	// Test with empty data
	_, result, err := engine.render("simple.tmpl", map[string]any{"Value": ""})
	if err != nil {
		t.Fatalf("render() with empty value should not fail: %v", err)
	}
	if result != "" {
		t.Errorf("Expected empty result, got '%s'", result)
	}

	// Test with nil data - template execution doesn't fail with nil data in Go templates
	// but accessing fields will result in <no value>
	_, result2, err2 := engine.render("simple.tmpl", nil)
	if err2 != nil {
		t.Fatalf("render() with nil data should not fail: %v", err2)
	}
	expected2 := "<no value>"
	if result2 != expected2 {
		t.Errorf("Expected '%s', got '%s'", expected2, result2)
	}
}

func TestEngine_SubjectTrimming(t *testing.T) {
	tmpDir := t.TempDir()
	
	// Create template with whitespace - using correct variable names that match RenderNewTicket
	subjectTemplate := "  [{{.TicketPrefix}}-#{{.TaskID}}] Test Subject  \n"
	err := os.WriteFile(filepath.Join(tmpDir, "new_ticket_subject.tmpl"), []byte(subjectTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create template: %v", err)
	}
	
	// Create minimal body template
	bodyTemplate := "Test body"
	err = os.WriteFile(filepath.Join(tmpDir, "new_ticket_body.tmpl"), []byte(bodyTemplate), 0644)
	if err != nil {
		t.Fatalf("Failed to create body template: %v", err)
	}
	
	engine, err := New(tmpDir)
	if err != nil {
		t.Fatalf("Failed to create engine: %v", err)
	}
	
	subject, _, err := engine.RenderNewTicket("ML", 123, "Test", "message", 4, 24)
	if err != nil {
		t.Fatalf("RenderNewTicket() should not fail: %v", err)
	}
	
	expected := "[ML-#123] Test Subject"
	if subject != expected {
		t.Errorf("Subject should be trimmed: expected '%s', got '%s'", expected, subject)
	}
}