package state

import (
	"path/filepath"
	"testing"
	"time"
)

func TestStore_EmailProcessing(t *testing.T) {
	// Create temporary database file
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	
	emailID := "test-email-123"
	
	// Initially, email should not be processed
	processed, err := store.IsProcessedEmail(emailID)
	if err != nil {
		t.Fatalf("IsProcessedEmail failed: %v", err)
	}
	if processed {
		t.Error("Email should not be processed initially")
	}
	
	// Mark email as processed
	err = store.MarkProcessedEmail(emailID)
	if err != nil {
		t.Fatalf("MarkProcessedEmail failed: %v", err)
	}
	
	// Now email should be marked as processed
	processed, err = store.IsProcessedEmail(emailID)
	if err != nil {
		t.Fatalf("IsProcessedEmail failed: %v", err)
	}
	if !processed {
		t.Error("Email should be processed after marking")
	}
}

func TestStore_OdooMessageTracking(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	
	messageID := int64(456)
	
	// Initially, message should not be sent
	sent := store.IsOdooMessageSent(messageID)
	if sent {
		t.Error("Message should not be sent initially")
	}
	
	// Mark message as sent
	err = store.MarkOdooMessageSent(messageID)
	if err != nil {
		t.Fatalf("MarkOdooMessageSent failed: %v", err)
	}
	
	// Now message should be marked as sent
	sent = store.IsOdooMessageSent(messageID)
	if !sent {
		t.Error("Message should be sent after marking")
	}
}

func TestStore_SlackMessageTracking(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	
	taskID := int64(789)
	slackMsg := SlackMessageInfo{
		Timestamp: "1234567890.123456",
		Channel:   "C1234567890",
	}
	
	// Initially, no Slack message should exist
	retrievedMsg, err := store.GetSlackMessage(taskID)
	if err != nil {
		t.Fatalf("GetSlackMessage failed: %v", err)
	}
	if retrievedMsg != nil {
		t.Error("No Slack message should exist initially")
	}
	
	// Store Slack message
	err = store.StoreSlackMessage(taskID, slackMsg)
	if err != nil {
		t.Fatalf("StoreSlackMessage failed: %v", err)
	}
	
	// Retrieve and verify Slack message
	retrievedMsg, err = store.GetSlackMessage(taskID)
	if err != nil {
		t.Fatalf("GetSlackMessage failed: %v", err)
	}
	if retrievedMsg == nil {
		t.Fatal("Slack message should exist after storing")
	}
	if retrievedMsg.Timestamp != slackMsg.Timestamp {
		t.Errorf("Expected timestamp %s, got %s", slackMsg.Timestamp, retrievedMsg.Timestamp)
	}
	if retrievedMsg.Channel != slackMsg.Channel {
		t.Errorf("Expected channel %s, got %s", slackMsg.Channel, retrievedMsg.Channel)
	}
}

func TestStore_SLAStateTracking(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	
	taskID := int64(999)
	now := time.Now()
	slaState := SLAState{
		TaskID:         taskID,
		CreatedAt:      now,
		StartSLABreach: false,
		EndSLABreach:   false,
	}
	
	// Initially, no SLA state should exist
	retrievedState, err := store.GetSLAState(taskID)
	if err != nil {
		t.Fatalf("GetSLAState failed: %v", err)
	}
	if retrievedState != nil {
		t.Error("No SLA state should exist initially")
	}
	
	// Store SLA state
	err = store.StoreSLAState(slaState)
	if err != nil {
		t.Fatalf("StoreSLAState failed: %v", err)
	}
	
	// Retrieve and verify SLA state
	retrievedState, err = store.GetSLAState(taskID)
	if err != nil {
		t.Fatalf("GetSLAState failed: %v", err)
	}
	if retrievedState == nil {
		t.Fatal("SLA state should exist after storing")
	}
	if retrievedState.TaskID != slaState.TaskID {
		t.Errorf("Expected task ID %d, got %d", slaState.TaskID, retrievedState.TaskID)
	}
	if retrievedState.StartSLABreach != slaState.StartSLABreach {
		t.Errorf("Expected StartSLABreach %v, got %v", slaState.StartSLABreach, retrievedState.StartSLABreach)
	}
}

func TestStore_LastOdooMessageTime(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	
	// Initially, should return zero time
	lastTime := store.GetLastOdooMessageTime()
	if !lastTime.IsZero() {
		t.Error("Last Odoo message time should be zero initially")
	}
	
	// Set a time
	testTime := time.Now().UTC().Truncate(time.Second)
	err = store.SetLastOdooMessageTime(testTime)
	if err != nil {
		t.Fatalf("SetLastOdooMessageTime failed: %v", err)
	}
	
	// Retrieve and verify
	retrievedTime := store.GetLastOdooMessageTime()
	if !retrievedTime.Equal(testTime) {
		t.Errorf("Expected time %v, got %v", testTime, retrievedTime)
	}
}

func TestStore_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	
	// Create store and add some data
	store, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	
	emailID := "persistent-test"
	err = store.MarkProcessedEmail(emailID)
	if err != nil {
		t.Fatalf("MarkProcessedEmail failed: %v", err)
	}
	
	store.Close()
	
	// Reopen store and verify data persists
	store2, err := New(dbPath)
	if err != nil {
		t.Fatalf("Failed to reopen store: %v", err)
	}
	defer store2.Close()
	
	processed, err := store2.IsProcessedEmail(emailID)
	if err != nil {
		t.Fatalf("IsProcessedEmail failed: %v", err)
	}
	if !processed {
		t.Error("Data should persist after reopening store")
	}
}