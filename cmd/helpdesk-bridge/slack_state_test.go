package main

import (
	"testing"

	"github.com/anaryk/odoo-helpdesk-bridge/internal/state"
)

// TestTaskReopenCloseCycle tests the scenario where a task goes Done→In Progress→Done again
// and verifies that the state management correctly allows both notifications
func TestTaskReopenCloseCycle(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := state.New(tmpDir + "/test.db")
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer func() { _ = store.Close() }()

	taskID := int64(99999)

	// Step 1: Task is initially completed
	// processCompletedTasks should process this and mark as closed
	if store.IsTaskClosedNotified(taskID) {
		t.Error("Task should not be marked as closed initially")
	}

	// Simulate marking task as closed (like processCompletedTasks does)
	err = store.MarkTaskClosedNotified(taskID)
	if err != nil {
		t.Fatalf("MarkTaskClosedNotified failed: %v", err)
	}

	if !store.IsTaskClosedNotified(taskID) {
		t.Error("Task should be marked as closed after MarkTaskClosedNotified")
	}

	// Step 2: Task is reopened (moved from Done to In Progress)
	// processReopenedTasks should process this, mark as reopened, and clear closed flag
	if store.IsTaskReopenedNotified(taskID) {
		t.Error("Task should not be marked as reopened initially")
	}

	// Simulate reopening (like processReopenedTasks does)
	err = store.MarkTaskReopenedNotified(taskID)
	if err != nil {
		t.Fatalf("MarkTaskReopenedNotified failed: %v", err)
	}

	// This is the key fix - clear the closed notification so it can be processed again
	err = store.ClearTaskClosedNotified(taskID)
	if err != nil {
		t.Fatalf("ClearTaskClosedNotified failed: %v", err)
	}

	if !store.IsTaskReopenedNotified(taskID) {
		t.Error("Task should be marked as reopened after MarkTaskReopenedNotified")
	}

	if store.IsTaskClosedNotified(taskID) {
		t.Error("Task should NOT be marked as closed after clearing in processReopenedTasks")
	}

	// Step 3: Task is completed again (moved from In Progress back to Done)
	// processCompletedTasks should process this again because IsTaskClosedNotified is now false

	// Verify that processCompletedTasks would not skip this task
	if store.IsTaskClosedNotified(taskID) {
		t.Error("processCompletedTasks would skip this task - this is the bug we're fixing!")
	}

	// Simulate second completion (like processCompletedTasks does)
	// With the fixed logic, these are always called regardless of email success
	err = store.MarkTaskClosedNotified(taskID)
	if err != nil {
		t.Fatalf("Second MarkTaskClosedNotified failed: %v", err)
	}

	// Clear reopened flag (like processCompletedTasks does)
	err = store.ClearTaskReopenedNotified(taskID)
	if err != nil {
		t.Fatalf("ClearTaskReopenedNotified failed: %v", err)
	}

	// Final state should be: closed=true, reopened=false
	if !store.IsTaskClosedNotified(taskID) {
		t.Error("Task should be marked as closed after second completion")
	}

	if store.IsTaskReopenedNotified(taskID) {
		t.Error("Task should not be marked as reopened after second completion")
	}

	// The key improvement: After marking as closed, processReopenedTasks
	// should not process this task again in the same cycle because
	// IsTaskReopenedNotified is now false and IsTaskClosedNotified is true

	t.Log("✅ Task reopen-close cycle works correctly!")
}
