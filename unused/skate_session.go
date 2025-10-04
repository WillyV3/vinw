package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

// saveSessionSkate saves session to Skate database
func saveSessionSkate(session *Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}

	key := fmt.Sprintf("session@vinw-%s", session.ID)
	cmd := exec.Command("skate", "set", key, string(data))
	return cmd.Run()
}

// loadSessionSkate loads session from Skate database
// Returns nil if session doesn't exist (not an error)
func loadSessionSkate(sessionID string) (*Session, error) {
	key := fmt.Sprintf("session@vinw-%s", sessionID)
	cmd := exec.Command("skate", "get", key)
	output, err := cmd.Output()
	if err != nil {
		// Session doesn't exist
		return nil, nil
	}

	var session Session
	if err := json.Unmarshal(output, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// sessionExistsSkate checks if a session exists in Skate
func sessionExistsSkate(sessionID string) bool {
	key := fmt.Sprintf("session@vinw-%s", sessionID)
	cmd := exec.Command("skate", "get", key)
	return cmd.Run() == nil
}

// deleteSessionSkate deletes a session from Skate
func deleteSessionSkate(sessionID string) error {
	key := fmt.Sprintf("session@vinw-%s", sessionID)
	cmd := exec.Command("skate", "delete", key)
	return cmd.Run()
}

// listSessionsSkate lists all vinw sessions in Skate
func listSessionsSkate() ([]string, error) {
	// List all databases
	cmd := exec.Command("skate", "list-dbs")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(output), "\n")
	var sessions []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for databases like "@vinw-{sessionID}"
		if strings.HasPrefix(line, "@vinw-") {
			// Extract session ID
			sessionID := strings.TrimPrefix(line, "@vinw-")
			if sessionID != "" {
				sessions = append(sessions, sessionID)
			}
		}
	}

	return sessions, nil
}
