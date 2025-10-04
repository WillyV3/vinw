package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetSessionDir(t *testing.T) {
	dir := getSessionDir()

	// Must contain .vinw/sessions
	if !strings.Contains(dir, ".vinw") {
		t.Errorf("session dir must contain .vinw, got: %s", dir)
	}
	if !strings.Contains(dir, "sessions") {
		t.Errorf("session dir must contain sessions, got: %s", dir)
	}

	// Must be absolute path
	if !filepath.IsAbs(dir) {
		t.Errorf("session dir must be absolute path, got: %s", dir)
	}

	// Directory must exist after calling getSessionDir
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Errorf("session dir should be created if not exists")
	}
}

func TestGetSessionID(t *testing.T) {
	sessionID := getSessionID()

	// Must not be empty
	if sessionID == "" {
		t.Error("session ID cannot be empty")
	}

	// If not in tmux, should be timestamp format
	// If in tmux, should start with %
	if !strings.HasPrefix(sessionID, "%") {
		// Timestamp format: 2025-10-03-153045
		if len(sessionID) != 19 {
			t.Errorf("timestamp session ID should be 19 chars, got: %d", len(sessionID))
		}
	}
}

func TestNewSession(t *testing.T) {
	sessionID := "test-session"
	rootPath := "/test/path"

	session := newSession(sessionID, rootPath)

	if session.ID != sessionID {
		t.Errorf("expected session ID %s, got %s", sessionID, session.ID)
	}

	if session.RootPath != rootPath {
		t.Errorf("expected root path %s, got %s", rootPath, session.RootPath)
	}

	if session.Changed == nil {
		t.Error("changed map should be initialized")
	}

	if session.StartTime.IsZero() {
		t.Error("start time should be set")
	}
}

func TestSaveAndLoadSession(t *testing.T) {
	// Create temp session dir for testing
	tempDir := t.TempDir()
	testSessionDir = tempDir

	sessionID := "test-save-load"
	session := newSession(sessionID, "/test")
	session.Changed["file1.go"] = true
	session.Changed["file2.go"] = true

	// Save
	err := saveSession(session)
	if err != nil {
		t.Fatalf("failed to save session: %v", err)
	}

	// Verify file exists
	sessionPath := filepath.Join(tempDir, sessionID+".json")
	if _, err := os.Stat(sessionPath); os.IsNotExist(err) {
		t.Error("session file should exist after save")
	}

	// Load
	loaded, err := loadSession(sessionID)
	if err != nil {
		t.Fatalf("failed to load session: %v", err)
	}

	if loaded.ID != session.ID {
		t.Errorf("loaded ID %s != saved ID %s", loaded.ID, session.ID)
	}

	if loaded.RootPath != session.RootPath {
		t.Errorf("loaded path %s != saved path %s", loaded.RootPath, session.RootPath)
	}

	if len(loaded.Changed) != 2 {
		t.Errorf("expected 2 changed files, got %d", len(loaded.Changed))
	}

	if !loaded.Changed["file1.go"] || !loaded.Changed["file2.go"] {
		t.Error("changed files not preserved")
	}
}

func TestSessionExists(t *testing.T) {
	tempDir := t.TempDir()
	testSessionDir = tempDir

	sessionID := "test-exists"

	// Should not exist initially
	if sessionExists(sessionID) {
		t.Error("session should not exist before creation")
	}

	// Create session
	session := newSession(sessionID, "/test")
	saveSession(session)

	// Should exist now
	if !sessionExists(sessionID) {
		t.Error("session should exist after save")
	}
}

func TestGetFileSizeColor(t *testing.T) {
	testDir := t.TempDir()

	tests := []struct {
		name      string
		lines     int
		expected  string
		colorName string
	}{
		{"small.go", 30, "42", "green"},
		{"medium.go", 75, "148", "yellow-green"},
		{"large.go", 125, "226", "yellow"},
		{"xlarge.go", 175, "214", "orange"},
		{"huge.go", 250, "196", "red"},
		{"boundary-49.go", 49, "42", "green"},
		{"boundary-50.go", 50, "148", "yellow-green"},
		{"boundary-100.go", 100, "226", "yellow"},
		{"boundary-150.go", 150, "214", "orange"},
		{"boundary-200.go", 200, "196", "red"},
	}

	for _, tt := range tests {
		filePath := filepath.Join(testDir, tt.name)
		content := strings.Repeat("line\n", tt.lines)
		if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
			t.Fatalf("failed to create test file: %v", err)
		}

		color := getFileSizeColor(filePath)
		if color != tt.expected {
			t.Errorf("getFileSizeColor(%q with %d lines) = %s, want %s (%s)",
				tt.name, tt.lines, color, tt.expected, tt.colorName)
		}
	}
}
