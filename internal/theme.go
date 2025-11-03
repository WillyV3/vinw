package internal

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/lipgloss"
	lipglossthemes "github.com/willyv3/gogh-themes/lipgloss"
)

// ThemeManager manages the current theme from gogh-themes
type ThemeManager struct {
	CurrentName string                // Current theme name
	Current     lipglossthemes.Theme  // Full theme with 16 colors as lipgloss.Color
	AllNames    []string              // All 361 theme names
	SessionID   string                // Session ID for Skate isolation
}

// NewThemeManager creates a new theme manager
func NewThemeManager() *ThemeManager {
	allNames := lipglossthemes.Names()

	// Try to load saved theme from Skate
	savedName := SavedTheme()
	if savedName != "" {
		if theme, ok := lipglossthemes.Get(savedName); ok {
			return &ThemeManager{
				CurrentName: savedName,
				Current:     theme,
				AllNames:    allNames,
			}
		}
	}

	// Default to Dracula
	theme, _ := lipglossthemes.Get("Dracula")
	return &ThemeManager{
		CurrentName: "Dracula",
		Current:     theme,
		AllNames:    allNames,
	}
}

// NewThemeManagerWithSession creates a new theme manager with a session ID
func NewThemeManagerWithSession(sessionID string) *ThemeManager {
	allNames := lipglossthemes.Names()

	// Try to load saved theme from Skate with session
	savedName := SavedThemeWithSession(sessionID)
	if savedName != "" {
		if theme, ok := lipglossthemes.Get(savedName); ok {
			return &ThemeManager{
				CurrentName: savedName,
				Current:     theme,
				AllNames:    allNames,
				SessionID:   sessionID,
			}
		}
	}

	// Default to Dracula
	theme, _ := lipglossthemes.Get("Dracula")
	return &ThemeManager{
		CurrentName: "Dracula",
		Current:     theme,
		AllNames:    allNames,
		SessionID:   sessionID,
	}
}

// NextTheme cycles to the next theme
func (tm *ThemeManager) NextTheme() {
	// Find current index
	currentIdx := 0
	for i, name := range tm.AllNames {
		if name == tm.CurrentName {
			currentIdx = i
			break
		}
	}

	// Cycle to next
	nextIdx := (currentIdx + 1) % len(tm.AllNames)
	tm.CurrentName = tm.AllNames[nextIdx]
	theme, _ := lipglossthemes.Get(tm.CurrentName)
	tm.Current = theme

	// Save theme to Skate (non-blocking)
	go tm.SaveTheme()
}

// PreviousTheme cycles to the previous theme
func (tm *ThemeManager) PreviousTheme() {
	// Find current index
	currentIdx := 0
	for i, name := range tm.AllNames {
		if name == tm.CurrentName {
			currentIdx = i
			break
		}
	}

	// Cycle to previous
	prevIdx := currentIdx - 1
	if prevIdx < 0 {
		prevIdx = len(tm.AllNames) - 1
	}
	tm.CurrentName = tm.AllNames[prevIdx]
	theme, _ := lipglossthemes.Get(tm.CurrentName)
	tm.Current = theme

	// Save theme to Skate (non-blocking)
	go tm.SaveTheme()
}

// SelectTheme sets a specific theme by name
func (tm *ThemeManager) SelectTheme(name string) bool {
	theme, ok := lipglossthemes.Get(name)
	if !ok {
		return false
	}

	tm.CurrentName = name
	tm.Current = theme

	// Save theme to Skate (non-blocking)
	go tm.SaveTheme()

	return true
}

// SaveTheme saves the current theme name to Skate for viewer synchronization
func (tm *ThemeManager) SaveTheme() {
	if tm.SessionID != "" {
		key := fmt.Sprintf("vinw-theme-name@%s", tm.SessionID)
		cmd := exec.Command("skate", "set", key, tm.CurrentName)
		cmd.Run()
	} else {
		cmd := exec.Command("skate", "set", "vinw-theme-name", tm.CurrentName)
		cmd.Run()
	}
}

// SavedTheme retrieves the saved theme name from Skate
func SavedTheme() string {
	cmd := exec.Command("skate", "get", "vinw-theme-name")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// SavedThemeWithSession retrieves the saved theme name from Skate with session
func SavedThemeWithSession(sessionID string) string {
	key := fmt.Sprintf("vinw-theme-name@%s", sessionID)
	cmd := exec.Command("skate", "get", key)
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// CurrentTheme s the current theme from Skate for viewer
func CurrentTheme() lipglossthemes.Theme {
	//  theme name
	cmd := exec.Command("skate", "get", "vinw-theme-name")
	nameBytes, _ := cmd.Output()
	name := strings.TrimSpace(string(nameBytes))

	//  theme by name
	if name != "" {
		if theme, ok := lipglossthemes.Get(name); ok {
			return theme
		}
	}

	// Default to Dracula if not found
	theme, _ := lipglossthemes.Get("Dracula")
	return theme
}

// CurrentThemeWithSession s the current theme from Skate for viewer with session
func CurrentThemeWithSession(sessionID string) lipglossthemes.Theme {
	//  theme name
	key := fmt.Sprintf("vinw-theme-name@%s", sessionID)
	cmd := exec.Command("skate", "get", key)
	nameBytes, _ := cmd.Output()
	name := strings.TrimSpace(string(nameBytes))

	//  theme by name
	if name != "" {
		if theme, ok := lipglossthemes.Get(name); ok {
			return theme
		}
	}

	// Default to Dracula if not found
	theme, _ := lipglossthemes.Get("Dracula")
	return theme
}

// CreateHeaderStyle creates a header style with the current theme
func (tm *ThemeManager) CreateHeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(tm.Current.Background).
		Foreground(tm.Current.Foreground).
		Bold(true).
		Padding(0, 1)
}

// ThemeDisplay returns a string showing current theme for display
func (tm *ThemeManager) ThemeDisplay() string {
	return tm.CurrentName
}
