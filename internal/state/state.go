// Package state provides persistent storage for the odoo-helpdesk-bridge application.
package state

import (
	"encoding/json"
	"time"

	"go.etcd.io/bbolt"
)

const (
	// dbFilePermissions defines the file permissions for the BBolt database file
	dbFilePermissions = 0600

	// int64ByteLength defines the byte length for int64 values
	int64ByteLength = 8

	// bitShiftOffset defines the bit shift offset for byte conversion
	bitShiftOffset = 56
)

var (
	bProcessedEmails  = []byte("processed_emails")
	bOdooMsgSent      = []byte("odoo_msg_sent")
	bLastOdooMsgTime  = []byte("last_odoo_msg_time")
	bClosedNotified   = []byte("closed_notified")
	bReopenedNotified = []byte("reopened_notified")
	bSlackMessages    = []byte("slack_messages")
	bSLAStates        = []byte("sla_states")
)

// Store provides persistent key-value storage using BBolt database.
type Store struct{ db *bbolt.DB }

// New creates a new Store instance with the specified database file path.
func New(path string) (*Store, error) {
	db, err := bbolt.Open(path, dbFilePermissions, nil)
	if err != nil {
		return nil, err
	}
	if err := db.Update(func(tx *bbolt.Tx) error {
		for _, b := range [][]byte{bProcessedEmails, bOdooMsgSent, bLastOdooMsgTime, bClosedNotified, bReopenedNotified, bSlackMessages, bSLAStates} {
			if _, e := tx.CreateBucketIfNotExists(b); e != nil {
				return e
			}
		}
		return nil
	}); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// Close closes the database connection.
func (s *Store) Close() error { return s.db.Close() }

// IsProcessedEmail checks if an email with the given ID has been processed.
func (s *Store) IsProcessedEmail(id string) (bool, error) {
	var ok bool
	err := s.db.View(func(tx *bbolt.Tx) error {
		ok = tx.Bucket(bProcessedEmails).Get([]byte(id)) != nil
		return nil
	})
	return ok, err
}

// MarkProcessedEmail marks an email as processed in the database.
func (s *Store) MarkProcessedEmail(id string) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bProcessedEmails).Put([]byte(id), []byte("1"))
	})
}

// IsOdooMessageSent checks if an Odoo message with the given ID has been sent.
func (s *Store) IsOdooMessageSent(id int64) bool {
	var ok bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		ok = tx.Bucket(bOdooMsgSent).Get(itob(id)) != nil
		return nil
	})
	return ok
}

// MarkOdooMessageSent marks an Odoo message as sent in the state store.
func (s *Store) MarkOdooMessageSent(id int64) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bOdooMsgSent).Put(itob(id), []byte("1"))
	})
}

// GetLastOdooMessageTime retrieves the timestamp of the last processed Odoo message.
func (s *Store) GetLastOdooMessageTime() time.Time {
	var t time.Time
	_ = s.db.View(func(tx *bbolt.Tx) error {
		if b := tx.Bucket(bLastOdooMsgTime).Get([]byte("ts")); b != nil {
			_ = t.UnmarshalText(b)
		}
		return nil
	})
	return t
}

// SetLastOdooMessageTime updates the timestamp of the last processed Odoo message.
func (s *Store) SetLastOdooMessageTime(t time.Time) error {
	txt, _ := t.UTC().MarshalText()
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bLastOdooMsgTime).Put([]byte("ts"), txt)
	})
}

// IsTaskClosedNotified checks if a task closure notification has been sent.
func (s *Store) IsTaskClosedNotified(id int64) bool {
	var ok bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		ok = tx.Bucket(bClosedNotified).Get(itob(id)) != nil
		return nil
	})
	return ok
}

// MarkTaskClosedNotified marks a task as having its closure notification sent.
func (s *Store) MarkTaskClosedNotified(id int64) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bClosedNotified).Put(itob(id), []byte("1"))
	})
}

// IsTaskReopenedNotified checks if a task reopened notification has been sent.
func (s *Store) IsTaskReopenedNotified(id int64) bool {
	var ok bool
	_ = s.db.View(func(tx *bbolt.Tx) error {
		ok = tx.Bucket(bReopenedNotified).Get(itob(id)) != nil
		return nil
	})
	return ok
}

// MarkTaskReopenedNotified marks a task as having its reopened notification sent.
func (s *Store) MarkTaskReopenedNotified(id int64) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bReopenedNotified).Put(itob(id), []byte("1"))
	})
}

// ClearTaskReopenedNotified removes the reopened notification flag for a task.
// This should be called when a task is closed again to allow future reopened notifications.
func (s *Store) ClearTaskReopenedNotified(id int64) error {
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bReopenedNotified).Delete(itob(id))
	})
}

// SlackMessageInfo stores Slack message details for threading
type SlackMessageInfo struct {
	Timestamp string `json:"timestamp"`
	Channel   string `json:"channel"`
}

// StoreSlackMessage saves Slack message info for a task
func (s *Store) StoreSlackMessage(taskID int64, msg SlackMessageInfo) error {
	data, _ := json.Marshal(msg)
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bSlackMessages).Put(itob(taskID), data)
	})
}

// GetSlackMessage retrieves Slack message info for a task
func (s *Store) GetSlackMessage(taskID int64) (*SlackMessageInfo, error) {
	var msg SlackMessageInfo
	err := s.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(bSlackMessages).Get(itob(taskID))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &msg)
	})
	if err != nil {
		return nil, err
	}
	if msg.Timestamp == "" {
		return nil, nil
	}
	return &msg, nil
}

// SLAState tracks SLA timing for a task
type SLAState struct {
	TaskID         int64      `json:"task_id"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	CompletedAt    *time.Time `json:"completed_at,omitempty"`
	StartSLABreach bool       `json:"start_sla_breach"`
	EndSLABreach   bool       `json:"end_sla_breach"`
}

// StoreSLAState saves SLA state for a task
func (s *Store) StoreSLAState(state SLAState) error {
	data, _ := json.Marshal(state)
	return s.db.Update(func(tx *bbolt.Tx) error {
		return tx.Bucket(bSLAStates).Put(itob(state.TaskID), data)
	})
}

// GetSLAState retrieves SLA state for a task
func (s *Store) GetSLAState(taskID int64) (*SLAState, error) {
	var state SLAState
	err := s.db.View(func(tx *bbolt.Tx) error {
		data := tx.Bucket(bSLAStates).Get(itob(taskID))
		if data == nil {
			return nil
		}
		return json.Unmarshal(data, &state)
	})
	if err != nil {
		return nil, err
	}
	if state.TaskID == 0 {
		return nil, nil
	}
	return &state, nil
}

func itob(v int64) []byte {
	b := make([]byte, int64ByteLength)
	for i := uint(0); i < int64ByteLength; i++ {
		b[i] = byte(v >> (bitShiftOffset - i*int64ByteLength))
	}
	return b
}
