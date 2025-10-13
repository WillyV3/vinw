package internal

import (
	"bufio"
	"os"
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

// countFileLines counts the number of lines in a file
func countFileLines(filePath string) int {
	file, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}
	return lineCount
}

// GetAllGitDiffs returns a map of file paths to lines added for all changed files
// This is much more efficient than calling git diff for each file
func GetAllGitDiffs() map[string]int {
	diffs := make(map[string]int)

	// Get unstaged changes
	cmd := exec.Command("git", "diff", "--numstat")
	output, err := cmd.Output()
	if err == nil {
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
	}

	// Get staged changes (these add to unstaged if same file)
	cmd = exec.Command("git", "diff", "--cached", "--numstat")
	output, err = cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				added, _ := strconv.Atoi(parts[0])
				filepath := parts[2]
				// Add to existing count if file has both staged and unstaged changes
				if existing, ok := diffs[filepath]; ok {
					diffs[filepath] = existing + added
				} else {
					diffs[filepath] = added
				}
			}
		}
	}

	// Get untracked files (mark as -1 to indicate new file without expensive line counting)
	cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
	output, err = cmd.Output()
	if err == nil {
		files := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, file := range files {
			if file != "" {
				// Mark as -1 to indicate "new file" without counting lines
				// This avoids expensive I/O for potentially hundreds of untracked files
				diffs[file] = -1
			}
		}
	}

	return diffs
}

// InitGitHub checks for git repo and offers to create one if needed
func InitGitHub(path string) error {
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