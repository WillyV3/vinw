package internal

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
)

// Theme represents a color theme
type Theme struct {
	Name        string
	HeaderBG    lipgloss.Color
	HeaderFG    lipgloss.Color
	Description string
}

// Available themes with muted, professional colors
var Themes = []Theme{
	{
		Name:        "Teal",
		HeaderBG:    lipgloss.Color("30"),   // Muted teal
		HeaderFG:    lipgloss.Color("230"),  // Light text
		Description: "Calm teal",
	},
	{
		Name:        "Purple",
		HeaderBG:    lipgloss.Color("54"),   // Muted purple
		HeaderFG:    lipgloss.Color("230"),
		Description: "Subtle purple",
	},
	{
		Name:        "Blue",
		HeaderBG:    lipgloss.Color("25"),   // Muted blue
		HeaderFG:    lipgloss.Color("230"),
		Description: "Classic blue",
	},
	{
		Name:        "Orange",
		HeaderBG:    lipgloss.Color("130"),  // Muted orange
		HeaderFG:    lipgloss.Color("230"),
		Description: "Warm orange",
	},
	{
		Name:        "Burnt",
		HeaderBG:    lipgloss.Color("94"),   // Burnt orange/brown
		HeaderFG:    lipgloss.Color("230"),
		Description: "Burnt sienna",
	},
	{
		Name:        "Slate",
		HeaderBG:    lipgloss.Color("240"),  // Slate gray
		HeaderFG:    lipgloss.Color("252"),
		Description: "Professional slate",
	},
	{
		Name:        "Forest",
		HeaderBG:    lipgloss.Color("22"),   // Forest green
		HeaderFG:    lipgloss.Color("230"),
		Description: "Forest green",
	},
	{
		Name:        "Mauve",
		HeaderBG:    lipgloss.Color("96"),   // Muted mauve
		HeaderFG:    lipgloss.Color("230"),
		Description: "Soft mauve",
	},
}

// ThemeManager manages the current theme
type ThemeManager struct {
	CurrentIndex int
	Current      Theme
	SessionID    string // Session ID for Skate isolation
}

// NewThemeManager creates a new theme manager
func NewThemeManager() *ThemeManager {
	// Try to load saved theme from Skate
	savedIndex := GetSavedTheme()
	if savedIndex >= 0 && savedIndex < len(Themes) {
		return &ThemeManager{
			CurrentIndex: savedIndex,
			Current:      Themes[savedIndex],
		}
	}

	// Default to first theme
	return &ThemeManager{
		CurrentIndex: 0,
		Current:      Themes[0],
	}
}

// NewThemeManagerWithSession creates a new theme manager with a session ID
func NewThemeManagerWithSession(sessionID string) *ThemeManager {
	// Try to load saved theme from Skate with session
	savedIndex := GetSavedThemeWithSession(sessionID)
	if savedIndex >= 0 && savedIndex < len(Themes) {
		return &ThemeManager{
			CurrentIndex: savedIndex,
			Current:      Themes[savedIndex],
			SessionID:    sessionID,
		}
	}

	// Default to first theme
	return &ThemeManager{
		CurrentIndex: 0,
		Current:      Themes[0],
		SessionID:    sessionID,
	}
}

// NextTheme cycles to the next theme
func (tm *ThemeManager) NextTheme() {
	tm.CurrentIndex = (tm.CurrentIndex + 1) % len(Themes)
	tm.Current = Themes[tm.CurrentIndex]
	tm.SaveTheme()
	tm.BroadcastTheme()
}

// PreviousTheme cycles to the previous theme
func (tm *ThemeManager) PreviousTheme() {
	tm.CurrentIndex--
	if tm.CurrentIndex < 0 {
		tm.CurrentIndex = len(Themes) - 1
	}
	tm.Current = Themes[tm.CurrentIndex]
	tm.SaveTheme()
	tm.BroadcastTheme()
}

// SaveTheme saves the current theme index to Skate
func (tm *ThemeManager) SaveTheme() {
	indexStr := fmt.Sprintf("%d", tm.CurrentIndex)
	if tm.SessionID != "" {
		key := fmt.Sprintf("vinw-theme-index@%s", tm.SessionID)
		cmd := exec.Command("skate", "set", key, indexStr)
		cmd.Run()
	} else {
		cmd := exec.Command("skate", "set", "vinw-theme-index", indexStr)
		cmd.Run()
	}
}

// BroadcastTheme broadcasts the theme change to viewer
func (tm *ThemeManager) BroadcastTheme() {
	// Run all skate commands in parallel for atomic-like update
	var wg sync.WaitGroup
	wg.Add(3)

	if tm.SessionID != "" {
		go func() {
			defer wg.Done()
			exec.Command("skate", "set", fmt.Sprintf("vinw-theme-bg@%s", tm.SessionID), string(tm.Current.HeaderBG)).Run()
		}()
		go func() {
			defer wg.Done()
			exec.Command("skate", "set", fmt.Sprintf("vinw-theme-fg@%s", tm.SessionID), string(tm.Current.HeaderFG)).Run()
		}()
		go func() {
			defer wg.Done()
			exec.Command("skate", "set", fmt.Sprintf("vinw-theme-name@%s", tm.SessionID), tm.Current.Name).Run()
		}()
	} else {
		go func() {
			defer wg.Done()
			exec.Command("skate", "set", "vinw-theme-bg", string(tm.Current.HeaderBG)).Run()
		}()
		go func() {
			defer wg.Done()
			exec.Command("skate", "set", "vinw-theme-fg", string(tm.Current.HeaderFG)).Run()
		}()
		go func() {
			defer wg.Done()
			exec.Command("skate", "set", "vinw-theme-name", tm.Current.Name).Run()
		}()
	}

	// Wait for all skate commands to complete
	wg.Wait()
}

// GetSavedTheme retrieves the saved theme index from Skate
func GetSavedTheme() int {
	cmd := exec.Command("skate", "get", "vinw-theme-index")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Parse the saved index
	indexStr := strings.TrimSpace(string(output))
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return 0
	}
	return index
}

// GetSavedThemeWithSession retrieves the saved theme index from Skate with session
func GetSavedThemeWithSession(sessionID string) int {
	key := fmt.Sprintf("vinw-theme-index@%s", sessionID)
	cmd := exec.Command("skate", "get", key)
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	// Parse the saved index
	indexStr := strings.TrimSpace(string(output))
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		return 0
	}
	return index
}

// GetCurrentTheme gets the current theme from Skate for viewer
func GetCurrentTheme() Theme {
	// Get theme name
	cmd := exec.Command("skate", "get", "vinw-theme-name")
	nameBytes, _ := cmd.Output()
	name := string(nameBytes)

	// Find theme by name
	for _, theme := range Themes {
		if theme.Name == name {
			return theme
		}
	}

	// Default to first theme if not found
	return Themes[0]
}

// CreateHeaderStyle creates a header style with the current theme
func (tm *ThemeManager) CreateHeaderStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Background(tm.Current.HeaderBG).
		Foreground(tm.Current.HeaderFG).
		Bold(true).
		Padding(0, 1)
}

// GetThemeDisplay returns a string showing current theme for display
func (tm *ThemeManager) GetThemeDisplay() string {
	return tm.Current.Name
}