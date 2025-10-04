package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// testSessionDir is used for testing to override the default session directory
var testSessionDir string

// Session represents a watching session
type Session struct {
	ID        string          `json:"id"`
	RootPath  string          `json:"root_path"`
	StartTime time.Time       `json:"start_time"`
	Changed   map[string]bool `json:"changed"`
}

// getSessionDir returns the cross-platform session directory
// Creates ~/.vinw/sessions/ if it doesn't exist
func getSessionDir() string {
	// Use test directory if set
	if testSessionDir != "" {
		return testSessionDir
	}

	home, err := os.UserHomeDir()
	if err != nil {
		// This should never happen on modern systems
		// But if it does, we MUST fail, not fallback
		panic("failed to get user home directory: " + err.Error())
	}

	sessionDir := filepath.Join(home, ".vinw", "sessions")

	// Create directory if it doesn't exist
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		panic("failed to create session directory: " + err.Error())
	}

	return sessionDir
}

// getSessionID returns the session identifier
// Tries tmux pane ID first, falls back to timestamp
func getSessionID() string {
	// Try to get tmux pane ID
	cmd := exec.Command("tmux", "display-message", "-p", "#D")
	if output, err := cmd.Output(); err == nil {
		paneID := strings.TrimSpace(string(output))
		if paneID != "" {
			return paneID
		}
	}

	// Fallback to timestamp
	return time.Now().Format("2006-01-02-150405")
}

// newSession creates a new session
func newSession(id string, rootPath string) *Session {
	return &Session{
		ID:        id,
		RootPath:  rootPath,
		StartTime: time.Now(),
		Changed:   make(map[string]bool),
	}
}

// sessionExists checks if a session file exists
func sessionExists(sessionID string) bool {
	sessionPath := filepath.Join(getSessionDir(), sessionID+".json")
	_, err := os.Stat(sessionPath)
	return err == nil
}

// loadSession loads a session from disk
func loadSession(sessionID string) (*Session, error) {
	sessionPath := filepath.Join(getSessionDir(), sessionID+".json")

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		return nil, err
	}

	var session Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// saveSession saves a session to disk
func saveSession(session *Session) error {
	sessionPath := filepath.Join(getSessionDir(), session.ID+".json")

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(sessionPath, data, 0644)
}

// getFileSizeIndicator returns a Bubble Tea-style indicator and color based on file line count
func getFileSizeIndicator(filePath string) (string, string) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		// Return empty indicator for unreadable files
		return "◦", "240"
	}

	lines := strings.Count(string(data), "\n")

	switch {
	case lines < 50:
		return "●", "42" // green dot for small files
	case lines < 100:
		return "◉", "148" // yellow-green circle for medium-small
	case lines < 150:
		return "◎", "226" // yellow double circle for medium
	case lines < 200:
		return "◈", "214" // orange diamond for large
	default:
		return "◆", "196" // red filled diamond for very large
	}
}
