package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"vinw/internal"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
)

// Styles
var (
	changedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")).
			Bold(true)

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))

	headerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Bold(true).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("243")).
			Padding(0, 1)
)

// Messages
type tickMsg time.Time
type clearCopyHintMsg struct{}

// Creation modes
type creationMode int

const (
	creationNone creationMode = iota
	creationFile
	creationDirectory
)

// Deletion state
type deletionState struct {
	path      string // Full path to delete
	isDir     bool   // Whether it's a directory
	itemCount int    // Number of items in directory (if applicable)
}

// Model
type model struct {
	rootPath       string
	tree           *tree.Tree
	treeString     string                 // Cached tree string
	treeLines      []string               // Cached tree lines
	maxLine        int                    // Cached max line number
	viewport       viewport.Model
	ready          bool
	width          int
	height         int
	diffCache      map[string]int         // Cache for git diff results
	lastContent    string                 // Track last content to avoid unnecessary updates
	gitignore      *internal.GitIgnore    // GitIgnore patterns
	respectIgnore  bool                   // Whether to respect .gitignore
	showHidden     bool                   // Whether to show hidden files and folders
	nestingEnabled bool                   // Whether to show nested directories (global toggle)
	expandedDirs   map[string]bool        // Track which directories are expanded (for manual expansion)
	selectedLine   int                    // Currently selected line in viewport
	fileMap        map[int]string         // Map of line number to file path
	dirMap         map[int]string         // Map of line number to directory path
	showHelp       bool                   // Whether to show help
	showViewer     bool                   // Whether to show viewer command popup
	showStartup    bool                   // Whether to show startup message
	creatingMode   creationMode           // Current creation mode (file/directory/none)
	textInput      textinput.Model        // Text input for file/directory names
	deletePending  *deletionState         // Pending deletion (nil if none)
	theme          *internal.ThemeManager // Theme manager
	sessionID      string                 // Unique session ID for this instance
	showCopyHint   bool                   // Whether to show "Copied!" hint
	copiedPath     string                 // Path that was copied (for display)
}

// updateTreeCache updates the cached tree string and related values
func (m *model) updateTreeCache() {
	m.treeString = m.tree.String()
	m.treeLines = strings.Split(m.treeString, "\n")
	m.maxLine = len(m.treeLines) - 1
	if m.maxLine < 0 {
		m.maxLine = 0
	}
}

func (m model) Init() tea.Cmd {
	return tick()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		verticalMargins := headerHeight + footerHeight

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMargins)
			m.viewport.YPosition = headerHeight
			// Rebuild tree with initial settings
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
			m.updateTreeCache()
			content := renderTreeWithSelection(m.treeString, m.selectedLine)
			m.viewport.SetContent(content)
			m.lastContent = content
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargins
		}

	case tea.KeyMsg:
		// If startup message is showing, handle special keys
		if m.showStartup {
			switch msg.String() {
			case "c":
				// Copy viewer command to clipboard
				viewerCmd := fmt.Sprintf("vinw-viewer %s", m.sessionID)
				copyCmd := exec.Command("pbcopy")
				copyCmd.Stdin = strings.NewReader(viewerCmd)
				copyCmd.Run() // Ignore errors, not all systems have pbcopy
				m.showStartup = false
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				// Dismiss startup on any other key
				m.showStartup = false
				return m, nil
			}
		}

		// If help is showing, any key dismisses it
		if m.showHelp {
			switch msg.String() {
			case "?":
				m.showHelp = false
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				// Dismiss help on any other key
				m.showHelp = false
			}
		}

		// If viewer popup is showing, handle special keys
		if m.showViewer {
			switch msg.String() {
			case "c":
				// Copy viewer command to clipboard
				viewerCmd := fmt.Sprintf("vinw-viewer %s", m.sessionID)
				copyCmd := exec.Command("pbcopy")
				copyCmd.Stdin = strings.NewReader(viewerCmd)
				copyCmd.Run() // Ignore errors, not all systems have pbcopy
				m.showViewer = false
				return m, nil
			case "v", "escape":
				m.showViewer = false
				return m, nil
			case "q", "ctrl+c":
				return m, tea.Quit
			default:
				// Dismiss viewer popup on any other key
				m.showViewer = false
			}
		}

		// If in creation mode, handle text input
		if m.creatingMode != creationNone {
			switch msg.String() {
			case "esc", "ctrl+c":
				// Cancel creation
				m.creatingMode = creationNone
				m.textInput.Reset()
				return m, nil
			case "enter":
				// Confirm creation
				name := strings.TrimSpace(m.textInput.Value())
				if name == "" {
					// Empty name, cancel
					m.creatingMode = creationNone
					m.textInput.Reset()
					return m, nil
				}

				// Determine target directory
				targetDir := m.rootPath
				if dirPath, ok := m.dirMap[m.selectedLine]; ok {
					// Selected line is a directory
					targetDir = filepath.Join(m.rootPath, dirPath)
				} else if filePath, ok := m.fileMap[m.selectedLine]; ok {
					// Selected line is a file, use its parent directory
					targetDir = filepath.Join(m.rootPath, filepath.Dir(filePath))
				}

				// Create file or directory
				fullPath := filepath.Join(targetDir, name)
				var err error
				if m.creatingMode == creationFile {
					err = internal.CreateFile(fullPath)
				} else {
					err = internal.CreateDirectory(fullPath)
				}

				// Reset creation mode
				m.creatingMode = creationNone
				m.textInput.Reset()

				if err != nil {
					// TODO: Show error to user - for now just silently fail and rebuild tree
					// Could add a status message field to model later
				}

				// Rebuild tree to show new file/directory
				m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
				m.updateTreeCache()
				newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(newContent)
				m.lastContent = newContent

				return m, nil
			default:
				// Handle text input
				var cmd tea.Cmd
				m.textInput, cmd = m.textInput.Update(msg)
				return m, cmd
			}
		}

		// If deletion is pending, handle confirmation
		if m.deletePending != nil {
			switch msg.String() {
			case "y", "Y":
				// Confirm deletion
				var err error
				if m.deletePending.isDir {
					err = internal.DeleteDirectory(m.deletePending.path)
				} else {
					err = internal.DeleteFile(m.deletePending.path)
				}

				// Clear pending deletion
				m.deletePending = nil

				if err != nil {
					// TODO: Show error to user
					// For now, just rebuild tree
				}

				// Rebuild tree to remove deleted item
				m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
				m.updateTreeCache()

				// Adjust selected line if needed
				if m.selectedLine > m.maxLine {
					m.selectedLine = m.maxLine
				}
				if m.selectedLine < 0 {
					m.selectedLine = 0
				}

				newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(newContent)
				m.lastContent = newContent

				return m, nil
			case "n", "N", "esc", "ctrl+c":
				// Cancel deletion
				m.deletePending = nil
				return m, nil
			}
		}

		switch msg.String() {
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "v":
			m.showViewer = !m.showViewer
			return m, nil
		case "c":
			// Copy path of selected file or directory to clipboard
			var pathToCopy string
			if dirPath, ok := m.dirMap[m.selectedLine]; ok {
				// Directory selected
				pathToCopy = filepath.Join(m.rootPath, dirPath)
			} else if filePath, ok := m.fileMap[m.selectedLine]; ok {
				// File selected
				pathToCopy = filepath.Join(m.rootPath, filePath)
			}

			if pathToCopy != "" {
				copyCmd := exec.Command("pbcopy")
				copyCmd.Stdin = strings.NewReader(pathToCopy)
				copyCmd.Run() // Ignore errors, not all systems have pbcopy

				// Show hint for 3 seconds
				m.showCopyHint = true
				m.copiedPath = filepath.Base(pathToCopy)
				return m, tea.Tick(3*time.Second, func(t time.Time) tea.Msg {
					return clearCopyHintMsg{}
				})
			}
			return m, nil
		case "r":
			// Manual git refresh (fast - updates diff markers only, no tree rebuild)
			m.diffCache = internal.GetAllGitDiffs()
			// Re-render tree with updated diff cache but same structure
			newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
			return m, nil
		case "R":
			// Full refresh (slow - rebuilds entire tree + git diff)
			m.diffCache = internal.GetAllGitDiffs()

			// Remember current selection
			var currentSelection string
			if f, ok := m.fileMap[m.selectedLine]; ok {
				currentSelection = f
			} else if d, ok := m.dirMap[m.selectedLine]; ok {
				currentSelection = d
			}

			// Rebuild entire tree
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
			m.updateTreeCache()

			// Try to maintain selection
			newSelectedLine := 0
			if currentSelection != "" {
				for line, file := range m.fileMap {
					if file == currentSelection {
						newSelectedLine = line
						break
					}
				}
				if newSelectedLine == 0 {
					for line, dir := range m.dirMap {
						if dir == currentSelection {
							newSelectedLine = line
							break
						}
					}
				}
			}

			// Ensure selected line is within bounds
			if newSelectedLine > m.maxLine {
				newSelectedLine = m.maxLine
			}
			if newSelectedLine < 0 {
				newSelectedLine = 0
			}
			m.selectedLine = newSelectedLine

			// Update viewport
			newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
			return m, nil
		case "q", "ctrl+c":
			return m, tea.Quit
		case "t":
			// Next theme
			m.theme.NextTheme()
			return m, nil
		case "T":
			// Previous theme
			m.theme.PreviousTheme()
			return m, nil
		case "i":
			// Toggle gitignore respect
			m.respectIgnore = !m.respectIgnore

			// Remember the currently selected file if one exists
			var currentFile string
			if f, ok := m.fileMap[m.selectedLine]; ok {
				currentFile = f
			}

			// Rebuild tree with new ignore setting
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
			m.updateTreeCache()

			// Try to find the same file in the new map
			newSelectedLine := 0
			if currentFile != "" {
				for line, file := range m.fileMap {
					if file == currentFile {
						newSelectedLine = line
						break
					}
				}
			}

			// Ensure selected line is within bounds
			if newSelectedLine > m.maxLine {
				newSelectedLine = m.maxLine
			}
			if newSelectedLine < 0 {
				newSelectedLine = 0
			}
			m.selectedLine = newSelectedLine

			// Update viewport with new selection
			newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
			return m, nil
		case "n":
			// Toggle directory nesting
			m.nestingEnabled = !m.nestingEnabled

			// Clear expanded directories when toggling nesting on/off
			if m.nestingEnabled {
				// When enabling full nesting, clear manual expansions
				m.expandedDirs = make(map[string]bool)
			}

			// Remember the currently selected file if one exists
			var currentFile string
			if f, ok := m.fileMap[m.selectedLine]; ok {
				currentFile = f
			}

			// Rebuild tree with new nesting setting
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
			m.updateTreeCache()

			// Try to find the same file in the new map
			newSelectedLine := 0
			if currentFile != "" {
				for line, file := range m.fileMap {
					if file == currentFile {
						newSelectedLine = line
						break
					}
				}
			}

			// Ensure selected line is within bounds
			if newSelectedLine > m.maxLine {
				newSelectedLine = m.maxLine
			}
			if newSelectedLine < 0 {
				newSelectedLine = 0
			}
			m.selectedLine = newSelectedLine

			// Update viewport with new selection
			newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
			return m, nil
		case "j", "down":
			// Move selection down using cached values
			if m.selectedLine < m.maxLine {
				m.selectedLine++
				// Update viewport with highlighted line
				content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(content)
				// Auto-scroll if needed
				if m.selectedLine >= m.viewport.YOffset+m.viewport.Height-1 {
					m.viewport.LineDown(1)
				}
			}
			return m, nil
		case "k", "up":
			// Move selection up using cached values
			if m.selectedLine > 0 {
				m.selectedLine--
				// Update viewport with highlighted line
				content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(content)
				// Auto-scroll if needed
				if m.selectedLine < m.viewport.YOffset {
					m.viewport.LineUp(1)
				}
			}
			return m, nil
		case "h":
			// Vim-style left: collapse directory (same as 'left' key)
			if !m.nestingEnabled {
				if dirPath, ok := m.dirMap[m.selectedLine]; ok {
					// Mark directory as collapsed
					delete(m.expandedDirs, dirPath)

					// Remember current selection
					var currentSelection string
					if f, ok := m.fileMap[m.selectedLine]; ok {
						currentSelection = f
					} else if d, ok := m.dirMap[m.selectedLine]; ok {
						currentSelection = d
					}

					// Rebuild tree with new expansion
					m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
					m.updateTreeCache()

					// Try to maintain selection
					newSelectedLine := m.selectedLine
					if currentSelection != "" {
						for line, file := range m.fileMap {
							if file == currentSelection {
								newSelectedLine = line
								break
							}
						}
						// Also check dirMap if not found in fileMap
						if newSelectedLine == m.selectedLine {
							for line, dir := range m.dirMap {
								if dir == currentSelection {
									newSelectedLine = line
									break
								}
							}
						}
					}

					// Ensure selected line is within bounds
					if newSelectedLine > m.maxLine {
						newSelectedLine = m.maxLine
					}
					if newSelectedLine < 0 {
						newSelectedLine = 0
					}
					m.selectedLine = newSelectedLine

					// Update viewport
					newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
					m.viewport.SetContent(newContent)
					m.lastContent = newContent
				}
			}
			return m, nil
		case "u":
			// Toggle hidden/unhidden files and folders
			m.showHidden = !m.showHidden

			// Remember the currently selected file if one exists
			var currentFile string
			if f, ok := m.fileMap[m.selectedLine]; ok {
				currentFile = f
			}

			// Rebuild tree with new hidden setting
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
			m.updateTreeCache()

			// Try to find the same file in the new map
			newSelectedLine := 0
			if currentFile != "" {
				for line, file := range m.fileMap {
					if file == currentFile {
						newSelectedLine = line
						break
					}
				}
			}

			// Ensure selected line is within bounds
			if newSelectedLine > m.maxLine {
				newSelectedLine = m.maxLine
			}
			if newSelectedLine < 0 {
				newSelectedLine = 0
			}
			m.selectedLine = newSelectedLine

			// Update viewport with new selection
			newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
			return m, nil
		case "right", "l":
			// Vim-style expand directory (l) or arrow key (→)
			if !m.nestingEnabled {
				if dirPath, ok := m.dirMap[m.selectedLine]; ok {
					// Mark directory as expanded
					m.expandedDirs[dirPath] = true

					// Remember current selection
					var currentSelection string
					if f, ok := m.fileMap[m.selectedLine]; ok {
						currentSelection = f
					} else if d, ok := m.dirMap[m.selectedLine]; ok {
						currentSelection = d
					}

					// Rebuild tree with new expansion
					m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
					m.updateTreeCache()

					// Try to maintain selection
					newSelectedLine := m.selectedLine
					if currentSelection != "" {
						for line, file := range m.fileMap {
							if file == currentSelection {
								newSelectedLine = line
								break
							}
						}
						// Also check dirMap if not found in fileMap
						if newSelectedLine == m.selectedLine {
							for line, dir := range m.dirMap {
								if dir == currentSelection {
									newSelectedLine = line
									break
								}
							}
						}
					}

					// Ensure selected line is within bounds
					if newSelectedLine > m.maxLine {
						newSelectedLine = m.maxLine
					}
					if newSelectedLine < 0 {
						newSelectedLine = 0
					}
					m.selectedLine = newSelectedLine

					// Update viewport
					newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
					m.viewport.SetContent(newContent)
					m.lastContent = newContent
				}
			}
			return m, nil
		case "left":
			// Collapse directory when nesting is disabled
			if !m.nestingEnabled {
				if dirPath, ok := m.dirMap[m.selectedLine]; ok {
					// Mark directory as collapsed
					delete(m.expandedDirs, dirPath)

					// Remember current selection
					var currentSelection string
					if f, ok := m.fileMap[m.selectedLine]; ok {
						currentSelection = f
					} else if d, ok := m.dirMap[m.selectedLine]; ok {
						currentSelection = d
					}

					// Rebuild tree with new expansion
					m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
					m.updateTreeCache()

					// Try to maintain selection
					newSelectedLine := m.selectedLine
					if currentSelection != "" {
						for line, file := range m.fileMap {
							if file == currentSelection {
								newSelectedLine = line
								break
							}
						}
						// Also check dirMap if not found in fileMap
						if newSelectedLine == m.selectedLine {
							for line, dir := range m.dirMap {
								if dir == currentSelection {
									newSelectedLine = line
									break
								}
							}
						}
					}

					// Ensure selected line is within bounds
					if newSelectedLine > m.maxLine {
						newSelectedLine = m.maxLine
					}
					if newSelectedLine < 0 {
						newSelectedLine = 0
					}
					m.selectedLine = newSelectedLine

					// Update viewport
					newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
					m.viewport.SetContent(newContent)
					m.lastContent = newContent
				}
			}
			return m, nil
		case "enter", " ":
			// Get the file at the selected line (only files are in the map, not directories)
			if filePath, ok := m.fileMap[m.selectedLine]; ok {
				fullPath := filepath.Join(m.rootPath, filePath)

				// Make sure it's actually a file, not a directory
				if info, err := os.Stat(fullPath); err == nil && !info.IsDir() {
					// Write to Skate for viewer to pick up, silently ignore errors
					key := fmt.Sprintf("vinw-current-file@%s", m.sessionID)
					cmd := exec.Command("skate", "set", key, fullPath)
					cmd.Run() // Ignore errors silently
				}
			}
			// If it's a directory or not in map, do nothing (directories aren't selectable)
			return m, nil
		case "a":
			// Create new file
			m.creatingMode = creationFile
			m.textInput = textinput.New()
			m.textInput.Placeholder = "filename.ext"
			m.textInput.Focus()
			m.textInput.CharLimit = 255
			m.textInput.Width = 50
			return m, nil
		case "A":
			// Create new directory
			m.creatingMode = creationDirectory
			m.textInput = textinput.New()
			m.textInput.Placeholder = "directory-name"
			m.textInput.Focus()
			m.textInput.CharLimit = 255
			m.textInput.Width = 50
			return m, nil
		case "d":
			// Delete file or directory
			var fullPath string
			var isDir bool

			// Check if selected line is a directory
			if dirPath, ok := m.dirMap[m.selectedLine]; ok {
				fullPath = filepath.Join(m.rootPath, dirPath)
				isDir = true
			} else if filePath, ok := m.fileMap[m.selectedLine]; ok {
				fullPath = filepath.Join(m.rootPath, filePath)
				isDir = false
			} else {
				// Nothing selected
				return m, nil
			}

			// Get item count if it's a directory
			itemCount := 0
			if isDir {
				count, err := internal.CountDirectoryContents(fullPath)
				if err == nil {
					itemCount = count
				}
			}

			// Set up deletion confirmation
			m.deletePending = &deletionState{
				path:      fullPath,
				isDir:     isDir,
				itemCount: itemCount,
			}

			return m, nil
		}

	case clearCopyHintMsg:
		m.showCopyHint = false
		m.copiedPath = ""
		return m, nil

	case tickMsg:
		// Update git diff cache efficiently with one call
		m.diffCache = internal.GetAllGitDiffs()

		// Remember the currently selected file if one exists
		var currentFile string
		if f, ok := m.fileMap[m.selectedLine]; ok {
			currentFile = f
		}

		// Rebuild tree with cached diff data and gitignore settings
		m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden)
		m.updateTreeCache()

		// Try to maintain selection on the same file
		if currentFile != "" {
			for line, file := range m.fileMap {
				if file == currentFile {
					m.selectedLine = line
					break
				}
			}
		}

		// Ensure selected line is within bounds
		if m.selectedLine > m.maxLine {
			m.selectedLine = m.maxLine
		}
		if m.selectedLine < 0 {
			m.selectedLine = 0
		}

		// Only update viewport if content has changed
		newContent := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
		if newContent != m.lastContent {
			m.viewport.SetContent(newContent)
			m.lastContent = newContent
		}

		return m, tick()
	}

	// Update viewport (handles scrolling)
	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}

	// Show startup message with viewer command
	if m.showStartup {
		startupText := fmt.Sprintf(`╭─────────────────────────────────────╮
│         Welcome to ⓥⓘⓝⓦ!            │
╰─────────────────────────────────────╯

Session ID: %s

To open the viewer, run in another terminal:

  vinw-viewer %s

Press 'c' to copy command to clipboard
Press any other key to continue...`, m.sessionID, m.sessionID)

		startupStyle := lipgloss.NewStyle().
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			startupStyle.Render(startupText),
		)
	}

	// Show viewer popup
	if m.showViewer {
		viewerText := fmt.Sprintf(`╭─────────────────────────────────────╮
│       Open Paired Viewer            │
╰─────────────────────────────────────╯

Run this command in another terminal:

  vinw-viewer %s

Session ID: %s

Press 'c' to copy command to clipboard
Press any other key to dismiss...`, m.sessionID, m.sessionID)

		viewerStyle := lipgloss.NewStyle().
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("42"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			viewerStyle.Render(viewerText),
		)
	}

	// Show creation prompt
	if m.creatingMode != creationNone {
		title := "Create New File"
		if m.creatingMode == creationDirectory {
			title = "Create New Directory"
		}

		// Determine target location for display
		targetPath := m.rootPath
		if dirPath, ok := m.dirMap[m.selectedLine]; ok {
			targetPath = filepath.Join(m.rootPath, dirPath)
		} else if filePath, ok := m.fileMap[m.selectedLine]; ok {
			targetPath = filepath.Join(m.rootPath, filepath.Dir(filePath))
		}

		// Shorten path for display
		displayPath := targetPath
		if home := os.Getenv("HOME"); home != "" && strings.HasPrefix(targetPath, home) {
			displayPath = "~" + strings.TrimPrefix(targetPath, home)
		}

		promptText := fmt.Sprintf(`%s

Location: %s

%s

enter: confirm • esc: cancel`, title, displayPath, m.textInput.View())

		promptStyle := lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("170"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			promptStyle.Render(promptText),
		)
	}

	// Show deletion confirmation
	if m.deletePending != nil {
		itemName := filepath.Base(m.deletePending.path)
		itemType := "file"
		warning := ""

		if m.deletePending.isDir {
			itemType = "directory"
			if m.deletePending.itemCount > 0 {
				warning = fmt.Sprintf("\n⚠  WARNING: This directory contains %d item(s)", m.deletePending.itemCount)
			} else {
				warning = "\n(empty directory)"
			}
		}

		confirmText := fmt.Sprintf(`⚠  Delete %s?

%s%s

This action cannot be undone!

y: confirm deletion • n/esc: cancel`, itemType, itemName, warning)

		confirmStyle := lipgloss.NewStyle().
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")) // Red for danger

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			confirmStyle.Render(confirmText),
		)
	}

	if m.showHelp {
		helpText := `╭─────────────────────────────────────╮
│          ⓥⓘⓝⓦ Help Guide            │
╰─────────────────────────────────────╯

Setup
─────
  Terminal 1    vinw
  Terminal 2    vinw-viewer

Navigation (Vim-style)
──────────────────────
  j, ↓          Move down
  k, ↑          Move up
  h, ←          Collapse directory
  l, →          Expand directory
  Space/Enter   Select file to view
  u             Toggle hidden files
  i             Toggle gitignore
  n             Toggle full nesting
  r             Refresh git status (fast)
  R             Full refresh (slow)
  a             Create new file
  A             Create new directory
  d             Delete file/directory
  c             Copy path to clipboard
  v             Show viewer command
  ?             Toggle this help
  q             Quit

Git Features
────────────
  • Shows uncommitted changes (+N)
  • Works without remote repos
  • Auto-creates GitHub repos

Press any key to dismiss...`

		helpStyle := lipgloss.NewStyle().
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			helpStyle.Render(helpText),
		)
	}

	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func shortenPath(path string) string {
	home := os.Getenv("HOME")
	if home != "" && strings.HasPrefix(path, home) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

func (m model) headerView() string {
	shortPath := shortenPath(m.rootPath)
	title := fmt.Sprintf("ⓥⓘⓝⓦ - %s", shortPath)

	// Add copy hint if active
	if m.showCopyHint {
		copyHintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("42")). // Green
			Bold(true)
		hint := copyHintStyle.Render(fmt.Sprintf(" [Copied: %s]", m.copiedPath))
		title = title + hint
	}

	// Use theme colors for header
	themedHeaderStyle := m.theme.CreateHeaderStyle()
	return themedHeaderStyle.Width(m.width).Render(title)
}

func (m model) footerView() string {
	ignoreStatus := "OFF"
	if m.respectIgnore {
		ignoreStatus = "ON"
	}
	hiddenStatus := "OFF"
	if m.showHidden {
		hiddenStatus = "ON"
	}
	nestStatus := "OFF"
	if m.nestingEnabled {
		nestStatus = "ON"
	}
	// Three lines for skinny layout
	line1 := fmt.Sprintf("j/k: nav | h/l: collapse/expand | u: hidden [%s] | r/R: refresh", hiddenStatus)
	line2 := fmt.Sprintf("i: git [%s] | n: nesting [%s] | t/T: theme [%s]", ignoreStatus, nestStatus, m.theme.Current.Name)
	line3 := "a: new file | A: new dir | d: delete | c: copy path | space/enter: select | ?: help | q: quit"
	info := line1 + "\n" + line2 + "\n" + line3
	return footerStyle.Width(m.width).Render(info)
}

func tick() tea.Cmd {
	// Reduced frequency: manual refresh with 'r' key is preferred for performance
	return tea.Tick(60*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// buildTree recursively builds a file tree with git diff tracking
func buildTree(rootPath string) *tree.Tree {
	return buildTreeRecursive(rootPath, "", nil, nil, false)
}

// buildTreeWithCache builds a file tree using cached git diff data
func buildTreeWithCache(rootPath string, diffCache map[string]int) *tree.Tree {
	return buildTreeRecursive(rootPath, "", diffCache, nil, false)
}

// buildTreeWithOptions builds a file tree with all options
func buildTreeWithOptions(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool) *tree.Tree {
	return buildTreeRecursive(rootPath, "", diffCache, gitignore, respectIgnore)
}

// buildTreeWithMap builds tree and returns a map of line numbers to file paths (deprecated, use buildTreeWithMaps)
func buildTreeWithMap(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, nestingEnabled bool) (*tree.Tree, map[int]string) {
	fileMap := make(map[int]string)
	lineNum := 1 // Start at 1 because the root directory takes line 0
	t := buildTreeRecursiveWithMap(rootPath, "", diffCache, gitignore, respectIgnore, nestingEnabled, make(map[string]bool), false, &lineNum, fileMap, nil)
	return t, fileMap
}

// buildTreeWithMaps builds tree and returns maps of line numbers to file paths and directory paths
func buildTreeWithMaps(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, nestingEnabled bool, expandedDirs map[string]bool, showHidden bool) (*tree.Tree, map[int]string, map[int]string) {
	fileMap := make(map[int]string)
	dirMap := make(map[int]string)
	lineNum := 1 // Start at 1 because the root directory takes line 0
	t := buildTreeRecursiveWithMap(rootPath, "", diffCache, gitignore, respectIgnore, nestingEnabled, expandedDirs, showHidden, &lineNum, fileMap, dirMap)
	return t, fileMap, dirMap
}

// renderTreeWithSelection renders tree with highlighted selected line
func renderTreeWithSelection(content string, selectedLine int) string {
	lines := strings.Split(content, "\n")
	if selectedLine >= 0 && selectedLine < len(lines) {
		// Highlight selected line with inverse colors
		highlightStyle := lipgloss.NewStyle().Reverse(true)
		lines[selectedLine] = highlightStyle.Render(lines[selectedLine])
	}
	return strings.Join(lines, "\n")
}

// renderTreeWithSelectionOptimized works with cached lines for better performance
func renderTreeWithSelectionOptimized(lines []string, selectedLine int) string {
	if len(lines) == 0 {
		return ""
	}

	if selectedLine < 0 || selectedLine >= len(lines) {
		return strings.Join(lines, "\n")
	}

	// Make a copy to avoid modifying the cached lines
	result := make([]string, len(lines))
	copy(result, lines)

	// Highlight selected line
	highlightStyle := lipgloss.NewStyle().Reverse(true)
	result[selectedLine] = highlightStyle.Render(lines[selectedLine])

	return strings.Join(result, "\n")
}

func buildTreeRecursiveWithMap(path string, relativePath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, nestingEnabled bool, expandedDirs map[string]bool, showHidden bool, lineNum *int, fileMap map[int]string, dirMap map[int]string) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName)

	entries, err := os.ReadDir(path)
	if err != nil {
		return t
	}

	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		relPath := filepath.Join(relativePath, entry.Name())
		entryName := entry.Name()

		// Always skip .git directory
		if entryName == ".git" {
			continue
		}

		// Check if this entry is hidden
		isHidden := strings.HasPrefix(entryName, ".")
		isGitignore := entryName == ".gitignore"

		// Skip hidden files and folders unless showHidden is enabled
		// Always show .gitignore regardless of showHidden setting
		if isHidden && !isGitignore && !showHidden {
			continue
		}

		// Check gitignore if enabled
		if respectIgnore && gitignore != nil && gitignore.IsIgnored(fullPath) {
			continue
		}

		if entry.IsDir() {
			// Track directory in dirMap at current line
			if dirMap != nil {
				dirMap[*lineNum] = relPath
			}
			*lineNum++

			// Determine if we should expand this directory
			shouldExpand := nestingEnabled || (expandedDirs != nil && expandedDirs[relPath])

			if shouldExpand {
				// Recursively build subtree - showHidden MUST be passed through
				subTree := buildTreeRecursiveWithMap(fullPath, relPath, diffCache, gitignore, respectIgnore, nestingEnabled, expandedDirs, showHidden, lineNum, fileMap, dirMap)
				t.Child(subTree)
			} else {
				// Show collapsed directory (including hidden directories when showHidden is true)
				dirStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("147"))
				displayName := entryName + "/"
				dirNameStyled := dirStyle.Render(displayName)
				t.Child(dirNameStyled)
			}
		} else {
			// Track file in fileMap at current line number
			fileMap[*lineNum] = relPath
			*lineNum++

			// Get git diff lines from cache
			var diffLines int
			if diffCache != nil {
				diffLines = diffCache[relPath]
			}

			// Style filename (including hidden files when showHidden is true)
			fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
			name := fileStyle.Render(entryName)

			// Add diff indicator if file has changes
			if diffLines > 0 {
				diffStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
				name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
			} else if diffLines == -1 {
				// New untracked file (marked as -1 to avoid expensive line counting)
				diffStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
				name = name + diffStyle.Render(" (new)")
			}

			t.Child(name)
		}
	}

	return t
}

func buildTreeRecursive(path string, relativePath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName)

	entries, err := os.ReadDir(path)
	if err != nil {
		return t
	}

	for _, entry := range entries {
		fullPath := filepath.Join(path, entry.Name())
		relPath := filepath.Join(relativePath, entry.Name())
		entryName := entry.Name()

		// Always skip .git directory
		if entryName == ".git" {
			continue
		}

		// Skip hidden files (except .gitignore)
		if strings.HasPrefix(entryName, ".") && entryName != ".gitignore" {
			continue
		}

		// Check gitignore if enabled
		if respectIgnore && gitignore != nil && gitignore.IsIgnored(fullPath) {
			continue
		}

		if entry.IsDir() {
			// Recursively build subtree
			subTree := buildTreeRecursive(fullPath, relPath, diffCache, gitignore, respectIgnore)
			t.Child(subTree)
		} else {
			// Get git diff lines from cache
			var diffLines int
			if diffCache != nil {
				diffLines = diffCache[relPath]
			}

			// Style filename (including hidden files when showHidden is true)
			fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
			name := fileStyle.Render(entryName)

			// Add diff indicator if file has changes
			if diffLines > 0 {
				diffStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
				name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
			} else if diffLines == -1 {
				// New untracked file (marked as -1 to avoid expensive line counting)
				diffStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
				name = name + diffStyle.Render(" (new)")
			}

			t.Child(name)
		}
	}

	return t
}

// generateSessionID creates a unique session ID based on the current directory
func generateSessionID(path string) string {
	// Use absolute path to ensure consistency
	absPath, _ := filepath.Abs(path)
	// Create a short hash of the path using Go's crypto/sha256
	hash := sha256.Sum256([]byte(absPath))
	// Return first 8 hex characters of the hash
	return fmt.Sprintf("%x", hash[:4]) // 4 bytes = 8 hex chars
}

func main() {
	// Check for benchmark mode
	benchmarkMode := false
	if len(os.Args) > 1 && os.Args[1] == "--benchmark" {
		benchmarkMode = true
		if len(os.Args) > 2 {
			os.Chdir(os.Args[2])
		}
	}

	// Get watch path from args or use current directory
	watchPath := "."
	if len(os.Args) > 1 && os.Args[1] != "--benchmark" {
		watchPath = os.Args[1]
	}

	// Get absolute path for everything
	absPath, _ := filepath.Abs(watchPath)
	watchPath = absPath // Use absolute path everywhere

	// Generate unique session ID for this directory
	sessionID := generateSessionID(absPath)

	// Build the viewer command
	viewerCmd := fmt.Sprintf("vinw-viewer %s", sessionID)

	// Print session info to terminal (copyable)
	fmt.Printf("vinw session started\n")
	fmt.Printf("Directory: %s\n", absPath)
	fmt.Printf("Session ID: %s\n", sessionID)
	fmt.Printf("\nTo open viewer, run this command in another terminal:\n")
	fmt.Printf("%s\n", viewerCmd)

	// Try to copy to clipboard
	copyCmd := exec.Command("pbcopy")
	copyCmd.Stdin = strings.NewReader(viewerCmd)
	if err := copyCmd.Run(); err == nil {
		fmt.Printf("\n✓ Command copied to clipboard! Just paste in a new terminal.\n")
	}
	fmt.Printf("\nStarting ⓥⓘⓝⓦ...\n\n")

	// Initialize theme manager with session ID FIRST
	themeManager := internal.NewThemeManagerWithSession(sessionID)
	themeManager.BroadcastTheme() // Broadcast initial theme to viewer

	// Initialize GitHub repo if needed (only on first run for this directory)
	if err := internal.InitGitHub(absPath); err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Load gitignore
	gitignore := internal.NewGitIgnore(watchPath)

	// Benchmark mode: Run performance tests and exit
	if benchmarkMode {
		fmt.Fprintf(os.Stderr, "\n=== vinw Performance Benchmark ===\n")
		fmt.Fprintf(os.Stderr, "Directory: %s\n", absPath)

		// Count files
		fileCount := 0
		filepath.Walk(absPath, func(path string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				fileCount++
			}
			return nil
		})
		fmt.Fprintf(os.Stderr, "Total files: %d\n\n", fileCount)

		// Benchmark git diff
		start := time.Now()
		diffCache := internal.GetAllGitDiffs()
		gitDiffTime := time.Since(start)
		fmt.Fprintf(os.Stderr, "Git diff time: %v\n", gitDiffTime)
		fmt.Fprintf(os.Stderr, "Files with changes: %d\n\n", len(diffCache))

		// Benchmark tree building (3 runs for average)
		var treeTimes []time.Duration
		for i := 0; i < 3; i++ {
			start = time.Now()
			_, _, _ = buildTreeWithMaps(watchPath, diffCache, gitignore, true, false, make(map[string]bool), false)
			elapsed := time.Since(start)
			treeTimes = append(treeTimes, elapsed)
			fmt.Fprintf(os.Stderr, "Tree build #%d: %v\n", i+1, elapsed)
		}

		// Calculate average
		var total time.Duration
		for _, t := range treeTimes {
			total += t
		}
		avg := total / time.Duration(len(treeTimes))
		fmt.Fprintf(os.Stderr, "Average tree build: %v\n\n", avg)

		fmt.Fprintf(os.Stderr, "=== Benchmark Complete ===\n")
		os.Exit(0)
	}

	// Get initial git diff cache
	initialDiffCache := internal.GetAllGitDiffs()

	// Build initial tree with gitignore support (default: ON) and nesting disabled (default: OFF)
	respectIgnore := true
	nestingEnabled := false // Nesting off by default for large repos
	showHidden := false // Hidden files/folders off by default
	expandedDirs := make(map[string]bool)
	tree, fileMap, dirMap := buildTreeWithMaps(watchPath, initialDiffCache, gitignore, respectIgnore, nestingEnabled, expandedDirs, showHidden)

	// Initialize model
	m := model{
		rootPath:       watchPath,
		tree:           tree,
		diffCache:      initialDiffCache,
		gitignore:      gitignore,
		respectIgnore:  respectIgnore,
		showHidden:     showHidden,
		nestingEnabled: nestingEnabled,
		expandedDirs:   expandedDirs,
		selectedLine:   0,
		fileMap:        fileMap,
		dirMap:         dirMap,
		theme:          themeManager,
		sessionID:      sessionID,
		showStartup:    true, // Show startup screen until user presses a key
	}

	// Initialize the cache
	m.updateTreeCache()
	initialContent := renderTreeWithSelectionOptimized(m.treeLines, 0)
	m.lastContent = initialContent

	// Run with fullscreen and mouse support
	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}
}
