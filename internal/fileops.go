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
