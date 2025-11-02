package sla

import (
	"context"
	"testing"
	"time"

	"github.com/anaryk/odoo-helpdesk-bridge/internal/config"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/state"
)

// MockOdooClient for testing
type MockOdooClient struct {
	tasks []*MockTask
}

type MockTask struct {
	ID        int64
	Name      string
	StageID   int64
	StageName string
}

type MockTaskMessage struct {
	ID     int64
	TaskID int64
}

func (m *MockOdooClient) ListRecentlyChangedTasks(ctx context.Context, since time.Time) ([]*MockTask, error) {
	// Return mock tasks - in real implementation this would be []*odoo.Task
	return m.tasks, nil
}

func (m *MockOdooClient) IsTaskDone(task *MockTask, doneStageIDs []int64) bool {
	for _, id := range doneStageIDs {
		if task.StageID == id {
			return true
		}
	}
	return false
}

func (m *MockOdooClient) MessagePostCustomer(ctx context.Context, taskID, customerPartnerID int64, body string) error {
	return nil
}

// MockSlackClient for testing
type MockSlackClient struct {
	notifications []MockNotification
}

type MockNotification struct {
	TaskID        int
	Title         string
	ViolationType string
}

func (m *MockSlackClient) NotifySLAViolation(parentMsg interface{}, taskID int, title string, violationType string) error {
	m.notifications = append(m.notifications, MockNotification{
		TaskID:        taskID,
		Title:         title,
		ViolationType: violationType,
	})
	return nil
}

func TestHandler_InitializeTask(t *testing.T) {
	// Create temporary state store
	tmpDir := t.TempDir()
	store, err := state.New(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}
	defer store.Close()

	cfg := &config.Config{
		App: config.App{
			SLA: config.SLA{
				StartTimeHours:      4,
				ResolutionTimeHours: 24,
			},
		},
	}

	handler := New(cfg, nil, nil, store)

	taskID := int64(123)
	err = handler.InitializeTask(taskID)
	if err != nil {
		t.Fatalf("InitializeTask failed: %v", err)
	}

	// Verify SLA state was created
	slaState, err := store.GetSLAState(taskID)
	if err != nil {
		t.Fatalf("GetSLAState failed: %v", err)
	}
	if slaState == nil {
		t.Fatal("SLA state should be created")
	}
	if slaState.TaskID != taskID {
		t.Errorf("Expected task ID %d, got %d", taskID, slaState.TaskID)
	}
	if slaState.CreatedAt.IsZero() {
		t.Error("Created timestamp should be set")
	}
}

func TestHandler_IsNewStage(t *testing.T) {
	cfg := &config.Config{
		App: config.App{
			SLA: config.SLA{
				StartTimeHours:      4,
				ResolutionTimeHours: 24,
			},
		},
	}

	handler := New(cfg, nil, nil, nil)

	tests := []struct {
		stageName string
		expected  bool
	}{
		{"new", true},
		{"nový", true},
		{"draft", true},
		{"návrh", true},
		{"backlog", true},
		{"in progress", false},
		{"done", false},
		{"closed", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.stageName, func(t *testing.T) {
			result := handler.isNewStage(tt.stageName)
			if result != tt.expected {
				t.Errorf("isNewStage(%q) = %v, want %v", tt.stageName, result, tt.expected)
			}
		})
	}
}

func TestSLAState_Serialization(t *testing.T) {
	// Test that SLAState can be properly serialized/deserialized
	tmpDir := t.TempDir()
	store, err := state.New(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}
	defer store.Close()

	now := time.Now().UTC().Truncate(time.Second)
	startTime := now.Add(1 * time.Hour)

	originalState := state.SLAState{
		TaskID:         456,
		CreatedAt:      now,
		StartedAt:      &startTime,
		CompletedAt:    nil,
		StartSLABreach: true,
		EndSLABreach:   false,
	}

	// Store the state
	err = store.StoreSLAState(originalState)
	if err != nil {
		t.Fatalf("StoreSLAState failed: %v", err)
	}

	// Retrieve and verify
	retrievedState, err := store.GetSLAState(456)
	if err != nil {
		t.Fatalf("GetSLAState failed: %v", err)
	}
	if retrievedState == nil {
		t.Fatal("Retrieved state should not be nil")
	}

	if retrievedState.TaskID != originalState.TaskID {
		t.Errorf("TaskID mismatch: got %d, want %d", retrievedState.TaskID, originalState.TaskID)
	}
	if !retrievedState.CreatedAt.Equal(originalState.CreatedAt) {
		t.Errorf("CreatedAt mismatch: got %v, want %v", retrievedState.CreatedAt, originalState.CreatedAt)
	}
	if retrievedState.StartedAt == nil || !retrievedState.StartedAt.Equal(*originalState.StartedAt) {
		t.Errorf("StartedAt mismatch: got %v, want %v", retrievedState.StartedAt, originalState.StartedAt)
	}
	if retrievedState.CompletedAt != nil {
		t.Errorf("CompletedAt should be nil, got %v", retrievedState.CompletedAt)
	}
	if retrievedState.StartSLABreach != originalState.StartSLABreach {
		t.Errorf("StartSLABreach mismatch: got %v, want %v", retrievedState.StartSLABreach, originalState.StartSLABreach)
	}
	if retrievedState.EndSLABreach != originalState.EndSLABreach {
		t.Errorf("EndSLABreach mismatch: got %v, want %v", retrievedState.EndSLABreach, originalState.EndSLABreach)
	}
}
