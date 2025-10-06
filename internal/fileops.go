package internal

import (
	"fmt"
	"os"
	"path/filepath"
)

// CreateFile creates a new file at the specified path
func CreateFile(fullPath string) error {
	// Check if file already exists
	if _, err := os.Stat(fullPath); err == nil {
		return fmt.Errorf("file already exists: %s", fullPath)
	}

	// Create the file
	file, err := os.Create(fullPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	return nil
}

// CreateDirectory creates a new directory at the specified path
func CreateDirectory(fullPath string) error {
	// Check if directory already exists
	if _, err := os.Stat(fullPath); err == nil {
		return fmt.Errorf("directory already exists: %s", fullPath)
	}

	// Create the directory with standard permissions
	if err := os.Mkdir(fullPath, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	return nil
}

// DeleteFile deletes a file at the specified path
func DeleteFile(fullPath string) error {
	// Check if file exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("file does not exist: %s", fullPath)
		}
		return fmt.Errorf("failed to stat file: %w", err)
	}

	// Safety check: ensure it's a file, not a directory
	if info.IsDir() {
		return fmt.Errorf("cannot delete directory as file: %s (use DeleteDirectory)", fullPath)
	}

	// Delete the file
	if err := os.Remove(fullPath); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	return nil
}

// DeleteDirectory deletes a directory and all its contents recursively
func DeleteDirectory(fullPath string) error {
	// Check if directory exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("directory does not exist: %s", fullPath)
		}
		return fmt.Errorf("failed to stat directory: %w", err)
	}

	// Safety check: ensure it's a directory
	if !info.IsDir() {
		return fmt.Errorf("not a directory: %s", fullPath)
	}

	// Delete the directory and all contents recursively
	if err := os.RemoveAll(fullPath); err != nil {
		return fmt.Errorf("failed to delete directory: %w", err)
	}

	return nil
}

// IsDirectoryEmpty checks if a directory is empty
func IsDirectoryEmpty(fullPath string) (bool, error) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return false, fmt.Errorf("failed to read directory: %w", err)
	}
	return len(entries) == 0, nil
}

// CountDirectoryContents returns the number of items in a directory (non-recursive)
func CountDirectoryContents(fullPath string) (int, error) {
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return 0, fmt.Errorf("failed to read directory: %w", err)
	}
	return len(entries), nil
}

// GetParentDirectory returns the parent directory of a given path
// If path is empty or is the root, returns the current directory
func GetParentDirectory(path string) string {
	if path == "" {
		return "."
	}
	parent := filepath.Dir(path)
	if parent == "." || parent == "/" {
		return path
	}
	return parent
}
