package main

import (
	"encoding/json"
	"os/exec"
	"testing"
	"time"
)

// Mock skate commands for testing
var mockSkateDB = make(map[string]string)
var useMockSkate = false

func TestSaveSessionSkate(t *testing.T) {
	session := &Session{
		ID:        "test-skate",
		RootPath:  "/test/path",
		StartTime: time.Now(),
		Changed: map[string]bool{
			"file1.go": true,
			"file2.go": true,
		},
	}

	// Save to skate
	err := saveSessionSkate(session)
	if err != nil {
		t.Fatalf("failed to save session to skate: %v", err)
	}

	// Verify it was saved (try to get it back)
	cmd := exec.Command("skate", "get", "session@vinw-"+session.ID)
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get session from skate: %v", err)
	}

	var loaded Session
	if err := json.Unmarshal(output, &loaded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if len(loaded.Changed) != 2 {
		t.Errorf("expected 2 files, got %d", len(loaded.Changed))
	}

	if !loaded.Changed["file1.go"] || !loaded.Changed["file2.go"] {
		t.Error("files not properly saved")
	}
}

func TestLoadSessionSkate(t *testing.T) {
	expected := &Session{
		ID:        "test-load",
		RootPath:  "/test/load",
		StartTime: time.Now(),
		Changed: map[string]bool{
			"main.go":   true,
			"README.md": true,
		},
	}

	// Save first
	data, _ := json.Marshal(expected)
	cmd := exec.Command("skate", "set", "session@vinw-"+expected.ID, string(data))
	if err := cmd.Run(); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Load
	loaded, err := loadSessionSkate(expected.ID)
	if err != nil {
		t.Fatalf("failed to load: %v", err)
	}

	if loaded == nil {
		t.Fatal("loaded session is nil")
	}

	if len(loaded.Changed) != len(expected.Changed) {
		t.Errorf("expected %d files, got %d", len(expected.Changed), len(loaded.Changed))
	}

	for file := range expected.Changed {
		if !loaded.Changed[file] {
			t.Errorf("file %s not loaded", file)
		}
	}
}

func TestSessionExistsSkate(t *testing.T) {
	sessionID := "test-exists-skate"

	// Clean up first to ensure clean state
	deleteSessionSkate(sessionID)

	// Should not exist initially
	if sessionExistsSkate(sessionID) {
		t.Error("session should not exist before creation")
	}

	// Create it
	session := &Session{
		ID:        sessionID,
		RootPath:  "/test",
		StartTime: time.Now(),
		Changed:   make(map[string]bool),
	}
	saveSessionSkate(session)

	// Should exist now
	if !sessionExistsSkate(sessionID) {
		t.Error("session should exist after creation")
	}
}

func TestDeleteSessionSkate(t *testing.T) {
	sessionID := "test-delete"

	// Create session
	session := &Session{
		ID:        sessionID,
		RootPath:  "/test",
		StartTime: time.Now(),
		Changed:   make(map[string]bool),
	}
	saveSessionSkate(session)

	// Delete
	err := deleteSessionSkate(sessionID)
	if err != nil {
		t.Fatalf("failed to delete: %v", err)
	}

	// Should not exist
	if sessionExistsSkate(sessionID) {
		t.Error("session should not exist after deletion")
	}
}

func TestListSessionsSkate(t *testing.T) {
	// Create multiple sessions
	sessions := []string{"session-1", "session-2", "session-3"}
	for _, s := range sessions {
		session := &Session{
			ID:        s,
			RootPath:  "/test",
			StartTime: time.Now(),
			Changed:   make(map[string]bool),
		}
		saveSessionSkate(session)
	}

	// List them
	list, err := listSessionsSkate()
	if err != nil {
		t.Fatalf("failed to list sessions: %v", err)
	}

	if len(list) < len(sessions) {
		t.Errorf("expected at least %d sessions, got %d", len(sessions), len(list))
	}

	// Verify our test sessions are in the list
	for _, s := range sessions {
		found := false
		for _, listed := range list {
			if listed == s {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("session %s not found in list", s)
		}
	}
}

func TestLoadNonExistentSessionSkate(t *testing.T) {
	sessionID := "does-not-exist-12345"

	// Should return nil, not error
	loaded, err := loadSessionSkate(sessionID)
	if err != nil {
		t.Errorf("loading non-existent session should not error: %v", err)
	}

	if loaded != nil {
		t.Errorf("expected nil session, got %v", loaded)
	}
}

// Cleanup test sessions
func TestCleanup(t *testing.T) {
	testSessions := []string{
		"test-skate", "test-load", "test-exists-skate",
		"test-delete", "session-1", "session-2", "session-3",
	}

	for _, s := range testSessions {
		deleteSessionSkate(s)
	}
}
