package main

import (
	"os/exec"
	"strconv"
	"strings"
)

// getGitDiffLines returns the number of lines added for a file
func getGitDiffLines(filePath string) int {
	cmd := exec.Command("git", "diff", "--numstat", "HEAD", "--", filePath)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	parts := strings.Fields(string(output))
	if len(parts) >= 1 {
		added, _ := strconv.Atoi(parts[0])
		return added
	}
	return 0
}

// initGitHub checks for git repo and offers to create one if needed
func initGitHub(path string) error {
	// Check if already in a git repo
	if isInGitRepo() {
		// Already have git, no need to create
		return nil
	}

	// Check if user previously declined for this directory
	if hasDeclinedRepo(path) {
		// User said no before, don't ask again
		return nil
	}

	// Check if GitHub CLI is available
	if !hasGitHubCLI() {
		// No GitHub CLI, can't create repo
		return nil
	}

	// Run the interactive Bubble Tea setup
	return runGitHubSetup(path)
}