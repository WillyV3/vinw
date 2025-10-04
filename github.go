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

// getAllGitDiffs returns a map of file paths to lines added for all changed files
// This is much more efficient than calling git diff for each file
func getAllGitDiffs() map[string]int {
	diffs := make(map[string]int)

	cmd := exec.Command("git", "diff", "--numstat", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return diffs
	}

	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			added, _ := strconv.Atoi(parts[0])
			filepath := parts[2]
			diffs[filepath] = added
		}
	}

	return diffs
}

// initGitHub checks for git repo and offers to create one if needed
func initGitHub(path string) error {
	// Check if we're in a git repo
	if isInGitRepo() {
		// Check if remote exists and is accessible
		if hasRemote() && !remoteExists() {
			// Local repo exists but remote is gone (probably deleted)
			// Clear any previous decline so we can offer to recreate
			clearRepoDeclined(path)

			// Check if GitHub CLI is available
			if !hasGitHubCLI() {
				return nil
			}

			// Offer to create a new remote repo
			return runGitHubSetupForBrokenRemote(path)
		}
		// Repo and remote are fine
		return nil
	}

	// No git repo exists - check if user previously declined
	if hasDeclinedRepo(path) {
		// User said no before, don't ask again
		return nil
	}

	// Check if GitHub CLI is available
	if !hasGitHubCLI() {
		// No GitHub CLI, can't create repo
		return nil
	}

	// Run the interactive Bubble Tea setup for new repo
	return runGitHubSetup(path)
}