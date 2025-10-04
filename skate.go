package main

import (
	"os/exec"
	"strings"
)

// hasDeclinedRepo checks if user has declined to create a repo for this directory
func hasDeclinedRepo(path string) bool {
	key := "vinw-declined-" + path
	cmd := exec.Command("skate", "get", key)
	return cmd.Run() == nil
}

// markRepoDeclined marks that user declined to create a repo for this directory
func markRepoDeclined(path string) {
	key := "vinw-declined-" + path
	cmd := exec.Command("skate", "set", key, "true")
	cmd.Run()
}

// clearRepoDeclined clears the declined status (useful if user changes their mind)
func clearRepoDeclined(path string) {
	key := "vinw-declined-" + path
	cmd := exec.Command("skate", "delete", key)
	cmd.Run()
}

// isInGitRepo checks if current directory is in a git repository
func isInGitRepo() bool {
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	return cmd.Run() == nil
}

// hasGitHubCLI checks if GitHub CLI is installed and authenticated
func hasGitHubCLI() bool {
	cmd := exec.Command("gh", "auth", "status")
	return cmd.Run() == nil
}

// getGitHubAccount returns the current GitHub account name
func getGitHubAccount() string {
	cmd := exec.Command("gh", "auth", "status")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		// Look for account line (format: "✓ Logged in to github.com account USERNAME")
		if strings.Contains(line, "account") && strings.Contains(line, "github.com") {
			// Extract username from parentheses or after "account"
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "account" && i+1 < len(parts) {
					account := parts[i+1]
					// Remove parentheses if present
					account = strings.TrimPrefix(account, "(")
					account = strings.TrimSuffix(account, ")")
					return account
				}
			}
		}
	}
	return ""
}