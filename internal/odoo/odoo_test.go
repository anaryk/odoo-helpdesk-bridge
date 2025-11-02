package odoo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewClient_Success(t *testing.T) {
	// Create mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Mock successful authentication
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  int64(42), // Mock user ID
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient() should not fail: %v", err)
	}

	if client == nil {
		t.Fatal("NewClient() should return a client")
	}
	if client.uid != 42 {
		t.Errorf("Expected uid 42, got %d", client.uid)
	}
	if client.cfg.DB != "testdb" {
		t.Errorf("Expected DB 'testdb', got '%s'", client.cfg.DB)
	}
}

func TestNewClient_AuthFailure(t *testing.T) {
	// Create mock server that returns authentication error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    1,
				"message": "Authentication failed",
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "baduser",
		Pass:    "badpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	_, err := NewClient(ctx, cfg)
	if err == nil {
		t.Error("NewClient() should fail with authentication error")
	}
	if !strings.Contains(err.Error(), "Authentication failed") {
		t.Errorf("Expected authentication error, got: %v", err)
	}
}

func TestCreateTask_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var response map[string]any

		switch callCount {
		case 1:
			// First call - authentication
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  int64(42),
			}
		case 2:
			// Second call - create task
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"result":  int64(123), // Mock task ID
			}
		default:
			// Third call - add follower
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      3,
				"result":  true,
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	input := CreateTaskInput{
		ProjectID:         10,
		Name:              "Test Task",
		Description:       "Test Description",
		CustomerPartnerID: 5,
	}

	taskID, err := client.CreateTask(ctx, input)
	if err != nil {
		t.Fatalf("CreateTask() should not fail: %v", err)
	}

	if taskID != 123 {
		t.Errorf("Expected task ID 123, got %d", taskID)
	}
}

func TestCreateTask_WithoutCustomer(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var response map[string]any

		if callCount == 1 {
			// Authentication
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  int64(42),
			}
		} else {
			// Create task without customer
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"result":  int64(456),
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	input := CreateTaskInput{
		ProjectID:         10,
		Name:              "Test Task",
		Description:       "Test Description",
		CustomerPartnerID: 0, // No customer
	}

	taskID, err := client.CreateTask(ctx, input)
	if err != nil {
		t.Fatalf("CreateTask() should not fail: %v", err)
	}

	if taskID != 456 {
		t.Errorf("Expected task ID 456, got %d", taskID)
	}

	// Should only have 2 calls (auth + create), no add follower call
	if callCount != 2 {
		t.Errorf("Expected 2 API calls, got %d", callCount)
	}
}

func TestFindOrCreatePartnerByEmail_ExistingPartner(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var response map[string]any

		if callCount == 1 {
			// Authentication
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  int64(42),
			}
		} else {
			// Search existing partner
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"result":  []int64{789}, // Existing partner ID
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	partnerID, err := client.FindOrCreatePartnerByEmail(ctx, "test@example.com", "Test User")
	if err != nil {
		t.Fatalf("FindOrCreatePartnerByEmail() should not fail: %v", err)
	}

	if partnerID != 789 {
		t.Errorf("Expected partner ID 789, got %d", partnerID)
	}
}

func TestFindOrCreatePartnerByEmail_CreateNew(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var response map[string]any

		switch callCount {
		case 1:
			// Authentication
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  int64(42),
			}
		case 2:
			// Search - no existing partner
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"result":  []int64{}, // No existing partner
			}
		default:
			// Create new partner
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      3,
				"result":  int64(999), // New partner ID
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	partnerID, err := client.FindOrCreatePartnerByEmail(ctx, "new@example.com", "New User")
	if err != nil {
		t.Fatalf("FindOrCreatePartnerByEmail() should not fail: %v", err)
	}

	if partnerID != 999 {
		t.Errorf("Expected partner ID 999, got %d", partnerID)
	}
}

func TestAddFollower(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var response map[string]any

		if callCount == 1 {
			// Authentication
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  int64(42),
			}
		} else {
			// Add follower
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      2,
				"result":  true,
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	client, err := NewClient(ctx, cfg)
	if err != nil {
		t.Fatalf("NewClient() failed: %v", err)
	}

	err = client.AddFollower(ctx, 123, 456)
	if err != nil {
		t.Fatalf("AddFollower() should not fail: %v", err)
	}
}

func TestRPC_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    500,
				"message": "Internal server error",
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	_, err := NewClient(ctx, cfg)
	if err == nil {
		t.Error("NewClient() should fail with server error")
	}
	if !strings.Contains(err.Error(), "Internal server error") {
		t.Errorf("Expected server error, got: %v", err)
	}
}

func TestRPC_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Return invalid JSON
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 5 * time.Second,
	}

	ctx := context.Background()
	_, err := NewClient(ctx, cfg)
	if err == nil {
		t.Error("NewClient() should fail with JSON decode error")
	}
}

func TestRPC_NetworkError(t *testing.T) {
	cfg := Config{
		URL:     "http://nonexistent.local:9999",
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 100 * time.Millisecond,
	}

	ctx := context.Background()
	_, err := NewClient(ctx, cfg)
	if err == nil {
		t.Error("NewClient() should fail with network error")
	}
}

func TestIfEmpty(t *testing.T) {
	tests := []struct {
		value    string
		fallback string
		expected string
	}{
		{"", "fallback", "fallback"},
		{"  ", "fallback", "fallback"},
		{"value", "fallback", "value"},
		{"  value  ", "fallback", "  value  "}, // preserves existing whitespace
	}

	for _, test := range tests {
		t.Run(fmt.Sprintf("value='%s'", test.value), func(t *testing.T) {
			result := ifEmpty(test.value, test.fallback)
			if result != test.expected {
				t.Errorf("Expected '%s', got '%s'", test.expected, result)
			}
		})
	}
}

func TestContext_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Simulate slow response
		time.Sleep(200 * time.Millisecond)
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  int64(42),
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	cfg := Config{
		URL:     server.URL,
		DB:      "testdb",
		User:    "testuser",
		Pass:    "testpass",
		Timeout: 100 * time.Millisecond, // Shorter than server delay
	}

	ctx := context.Background()
	_, err := NewClient(ctx, cfg)
	if err == nil {
		t.Error("NewClient() should fail with timeout error")
	}
}

func TestSetTaskStage_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  true,
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		cfg:  Config{URL: server.URL, DB: "testdb"},
		uid:  42,
		http: &http.Client{}, // Add HTTP client
	}

	err := client.SetTaskStage(context.Background(), 123, 456)
	if err != nil {
		t.Errorf("SetTaskStage() should not fail: %v", err)
	}
}

func TestAssignTask_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var response map[string]any
		if callCount == 1 {
			// First call: search for user - return user ID
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  []int64{123}, // Found user with ID 123
			}
		} else {
			// Second call: update task - return success
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  true,
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		cfg:  Config{URL: server.URL, DB: "testdb"},
		uid:  42,
		http: &http.Client{},
	}

	err := client.AssignTask(context.Background(), 123, "test@example.com")
	if err != nil {
		t.Errorf("AssignTask() should not fail: %v", err)
	}
}

func TestGetTaskCounts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Mock response for search_count
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  5, // 5 tasks assigned to user
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		cfg:  Config{URL: server.URL, DB: "testdb"},
		uid:  42,
		http: &http.Client{},
	}

	operatorEmails := []string{"operator1@test.com", "operator2@test.com"}
	counts, err := client.GetTaskCounts(context.Background(), 11, operatorEmails)
	if err != nil {
		t.Errorf("GetTaskCounts() should not fail: %v", err)
	}
	if len(counts) != 2 {
		t.Errorf("Expected 2 counts, got %d", len(counts))
	}
}

func TestIsTaskDone_WithDoneStageIDs(t *testing.T) {
	client := &Client{}
	task := &Task{StageID: 103}

	// Test with matching stage ID
	doneStageIDs := []int64{101, 102, 103}
	if !client.IsTaskDone(task, doneStageIDs) {
		t.Error("Task should be considered done with matching stage ID")
	}

	// Test with non-matching stage ID
	doneStageIDs = []int64{101, 102}
	if client.IsTaskDone(task, doneStageIDs) {
		t.Error("Task should not be considered done with non-matching stage ID")
	}
}

func TestIsTaskDone_WithoutDoneStageIDs(t *testing.T) {
	client := &Client{}

	// Test legacy behavior when no done_stage_ids configured
	doneTask := &Task{StageName: "Done"}
	if !client.IsTaskDone(doneTask, []int64{}) {
		t.Error("Task with 'Done' stage should be considered done")
	}

	resolvedTask := &Task{StageName: "Resolved"}
	if !client.IsTaskDone(resolvedTask, []int64{}) {
		t.Error("Task with 'Resolved' stage should be considered done")
	}

	hotovyTask := &Task{StageName: "Hotovo"}
	if !client.IsTaskDone(hotovyTask, []int64{}) {
		t.Error("Task with 'Hotovo' stage should be considered done")
	}

	openTask := &Task{StageName: "In Progress"}
	if client.IsTaskDone(openTask, []int64{}) {
		t.Error("Task with 'In Progress' stage should not be considered done")
	}
}

func TestUploadAttachment_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  int64(42), // Attachment ID
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		cfg:  Config{URL: server.URL, DB: "testdb"},
		uid:  42,
		http: &http.Client{},
	}

	attachment, err := client.UploadAttachment(context.Background(), 123, "test.pdf", "application/pdf", []byte("test data"))
	if err != nil {
		t.Errorf("UploadAttachment() should not fail: %v", err)
	}

	if attachment.ID != 42 {
		t.Errorf("Expected attachment ID 42, got %d", attachment.ID)
	}
	if attachment.Name != "test.pdf" {
		t.Errorf("Expected attachment name 'test.pdf', got %s", attachment.Name)
	}
}

func TestGetTaskAttachments_Success(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++
		var response map[string]any
		if callCount == 1 {
			// First call: search for attachments - return IDs
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  []int64{10, 20},
			}
		} else {
			// Second call: read attachment details
			response = map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": []map[string]any{
					{"id": float64(10), "name": "doc1.pdf", "mimetype": "application/pdf", "file_size": float64(1024)},
					{"id": float64(20), "name": "img1.jpg", "mimetype": "image/jpeg", "file_size": float64(2048)},
				},
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		cfg:  Config{URL: server.URL, DB: "testdb"},
		uid:  42,
		http: &http.Client{},
	}

	attachments, err := client.GetTaskAttachments(context.Background(), 123)
	if err != nil {
		t.Errorf("GetTaskAttachments() should not fail: %v", err)
	}

	if len(attachments) != 2 {
		t.Errorf("Expected 2 attachments, got %d", len(attachments))
	}

	if attachments[0].Name != "doc1.pdf" {
		t.Errorf("Expected first attachment name 'doc1.pdf', got %s", attachments[0].Name)
	}
}

func TestReopenTask_TaskAlreadyOpen(t *testing.T) {
	// Mock server that returns an open task
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++

		switch callCount {
		case 1:
			// First call: GetTask returns open task
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": []map[string]any{
					{
						"id":         int64(123),
						"name":       "Test Task",
						"stage_id":   []any{int64(1), "New"},
						"partner_id": []any{int64(456), "Test Customer"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		case 2:
			// Second call: partnerEmailName
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": []map[string]any{
					{
						"email": "test@example.com",
						"name":  "Test Customer",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			t.Errorf("Unexpected call count: %d", callCount)
		}
	}))
	defer server.Close()

	client := &Client{
		cfg: Config{
			URL: server.URL,
			DB:  "test",
		},
		uid:  42,
		http: &http.Client{},
	}

	// Should not make any stage change since task is already open
	wasReopened, err := client.ReopenTask(context.Background(), 123, 1)
	if err != nil {
		t.Errorf("ReopenTask() should not fail for open task: %v", err)
	}
	if wasReopened {
		t.Errorf("ReopenTask() should return false for already open task")
	}

	if callCount != 2 {
		t.Errorf("Expected 2 calls (GetTask + partnerEmailName), got %d", callCount)
	}
}

func TestReopenTask_TaskClosed(t *testing.T) {
	// Mock server that handles task reopening
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		callCount++

		switch callCount {
		case 1:
			// First call: GetTask returns closed task
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": []map[string]any{
					{
						"id":         int64(123),
						"name":       "Test Task",
						"stage_id":   []any{int64(5), "Done"},
						"partner_id": []any{int64(456), "Test Customer"},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		case 2:
			// Second call: partnerEmailName
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": []map[string]any{
					{
						"email": "test@example.com",
						"name":  "Test Customer",
					},
				},
			}
			_ = json.NewEncoder(w).Encode(response)
		case 3:
			// Third call: SetTaskStage
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  true,
			}
			_ = json.NewEncoder(w).Encode(response)
		case 4:
			// Fourth call: message_post for reopening comment
			response := map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result":  true,
			}
			_ = json.NewEncoder(w).Encode(response)
		default:
			t.Errorf("Unexpected call count: %d", callCount)
		}
	}))
	defer server.Close()

	client := &Client{
		cfg: Config{
			URL: server.URL,
			DB:  "test",
		},
		uid:  42,
		http: &http.Client{},
	}

	wasReopened, err := client.ReopenTask(context.Background(), 123, 1)
	if err != nil {
		t.Errorf("ReopenTask() should not fail: %v", err)
	}
	if !wasReopened {
		t.Errorf("ReopenTask() should return true for closed task")
	}

	if callCount != 4 {
		t.Errorf("Expected 4 calls (GetTask, partnerEmailName, SetTaskStage, message_post), got %d", callCount)
	}
}

func TestReopenTask_GetTaskFails(t *testing.T) {
	// Mock server that returns error for GetTask
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		response := map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]any{
				"code":    100,
				"message": "Task not found",
			},
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := &Client{
		cfg: Config{
			URL: server.URL,
			DB:  "test",
		},
		uid:  42,
		http: &http.Client{},
	}

	wasReopened, err := client.ReopenTask(context.Background(), 123, 1)
	if err == nil {
		t.Error("ReopenTask() should fail when GetTask fails")
	}
	if wasReopened {
		t.Error("ReopenTask() should return false when failing")
	}

	if !strings.Contains(err.Error(), "failed to get task") {
		t.Errorf("Expected 'failed to get task' error, got: %v", err)
	}
}
