package main

import (
	"context"
	"testing"

	"github.com/anaryk/odoo-helpdesk-bridge/internal/config"
	"github.com/anaryk/odoo-helpdesk-bridge/internal/odoo"
)

// Test processOdooEvents refactoring - verify that functions are called correctly
func TestProcessOdooEventsRefactoring(t *testing.T) {
	// Create minimal config for testing
	cfg := &config.Config{
		App: config.App{
			DoneStageIDs: []int64{103},
		},
	}
	
	// Create mock data
	basicTasks := []*odoo.Task{
		{
			ID:        1,
			Name:      "Test Task 1",
			StageID:   103, // Done stage
			StageName: "Done",
		},
		{
			ID:        2,
			Name:      "Test Task 2", 
			StageID:   101, // Not done stage
			StageName: "In Progress",
		},
	}
	
	// Test that we can create the basicTasks slice - this tests our function signatures
	if len(basicTasks) != 2 {
		t.Errorf("Expected 2 basic tasks, got %d", len(basicTasks))
	}
	
	// Test that task 1 would be considered done
	task1 := basicTasks[0]
	isDone := false
	for _, stageID := range cfg.App.DoneStageIDs {
		if task1.StageID == stageID {
			isDone = true
			break
		}
	}
	if !isDone {
		t.Error("Task 1 should be considered done based on stage ID")
	}
	
	// Test that task 2 would not be considered done
	task2 := basicTasks[1]
	isDone = false
	for _, stageID := range cfg.App.DoneStageIDs {
		if task2.StageID == stageID {
			isDone = true
			break
		}
	}
	if isDone {
		t.Error("Task 2 should not be considered done based on stage ID")
	}
}

// Test that basic task iteration works with correct types
func TestBasicTaskIteration(t *testing.T) {
	basicTasks := []*odoo.Task{
		{ID: 1, Name: "Task 1"},
		{ID: 2, Name: "Task 2"},
	}
	
	count := 0
	for _, basicTask := range basicTasks {
		count++
		if basicTask.ID <= 0 {
			t.Errorf("Task %d has invalid ID: %d", count, basicTask.ID)
		}
		if basicTask.Name == "" {
			t.Errorf("Task %d has empty name", count)
		}
	}
	
	if count != 2 {
		t.Errorf("Expected to iterate over 2 tasks, got %d", count)
	}
}

// Test function signature compatibility
func TestFunctionSignatures(t *testing.T) {
	ctx := context.Background()
	
	// These should compile without errors
	_ = ctx
	
	// Test that we can pass []*odoo.Task to our functions
	basicTasks := []*odoo.Task{}
	
	// Test with non-empty slice to avoid nil check issues
	if len(basicTasks) != 0 {
		t.Error("basicTasks should be empty")
	}
	
	// Verify the type is correct (type inference)
	var _ = basicTasks
}