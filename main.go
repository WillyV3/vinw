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
	"vinw/internal/findreplace"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/tree"
	lipglossthemes "github.com/willyv3/gogh-themes/lipgloss"
	"github.com/sahilm/fuzzy"
)

// Messages
type tickMsg time.Time
type clearCopyHintMsg struct{}

// Symlink support - track visited paths to prevent infinite loops
type visitedPaths struct {
	paths map[string]bool
}

func newVisitedPaths() *visitedPaths {
	return &visitedPaths{
		paths: make(map[string]bool),
	}
}

func (v *visitedPaths) visit(path string) bool {
	// Resolve to canonical path to detect loops
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		// If we can't resolve, treat as unvisited (might be broken symlink)
		canonical = path
	}

	if v.paths[canonical] {
		return false // Already visited (loop detected)
	}
	v.paths[canonical] = true
	return true
}

// Symlink helper functions
func isSymlink(entry os.DirEntry) bool {
	return entry.Type()&os.ModeSymlink != 0
}

func getSymlinkTarget(fullPath string) (string, error) {
	return os.Readlink(fullPath)
}

func isSymlinkToDir(fullPath string) (bool, bool, error) {
	// Use Stat (follows symlink) to check target
	info, err := os.Stat(fullPath)
	if err != nil {
		// Broken symlink
		return false, true, err
	}
	return info.IsDir(), false, nil
}

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

// Search result
type searchResult struct {
	lineNum    int    // Line number in tree
	path       string // Relative path
	matchScore int    // Fuzzy match score
	isDir      bool   // Whether it's a directory
}

// Model
type model struct {
	rootPath          string
	tree              *tree.Tree
	treeString        string   // Cached tree string
	treeLines         []string // Cached tree lines
	maxLine           int      // Cached max line number
	viewport          viewport.Model
	ready             bool
	width             int
	height            int
	diffCache         map[string]int         // Cache for git diff results
	lastContent       string                 // Track last content to avoid unnecessary updates
	gitignore         *internal.GitIgnore    // GitIgnore patterns
	respectIgnore     bool                   // Whether to respect .gitignore
	showHidden        bool                   // Whether to show hidden files and folders
	nestingEnabled    bool                   // Whether to show nested directories (global toggle)
	expandedDirs      map[string]bool        // Track which directories are expanded (for manual expansion)
	selectedLine      int                    // Currently selected line in viewport
	fileMap           map[int]string         // Map of line number to file path
	dirMap            map[int]string         // Map of line number to directory path
	showHelp          bool                   // Whether to show help
	showViewer        bool                   // Whether to show viewer command popup
	showStartup       bool                   // Whether to show startup message
	creatingMode      creationMode           // Current creation mode (file/directory/none)
	textInput         textinput.Model        // Text input for file/directory names
	deletePending     *deletionState         // Pending deletion (nil if none)
	theme             *internal.ThemeManager // Theme manager
	sessionID         string                 // Unique session ID for this instance
	showCopyHint      bool                   // Whether to show "Copied!" hint
	copiedPath        string                 // Path that was copied (for display)
	lastKeyWasG       bool                   // Track if last key was 'g' for gg detection
	lastKeyTime       time.Time              // Time of last 'g' key press
	searchMode        bool                   // Whether in search mode
	searchInput       textinput.Model        // Text input for search query
	searchResults     []searchResult         // Current search results
	findReplaceMode   bool                   // Whether in find/replace mode
	findReplaceModel  findreplace.Model      // Find/replace model
	searchSelectedIdx int                    // Selected result index
	searchViewport    viewport.Model         // Viewport for search results
	moveMode          bool                   // Whether in move mode
	moveSource        string                 // Full path of file/folder being moved
	themePickerOpen   bool                   // Whether theme picker is open
	themeFilterInput  textinput.Model        // Text input for theme search
	themeFilteredList []string               // Filtered theme names
	themeSelectedIdx  int                    // Selected theme in picker
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

	// If in find/replace mode, delegate ALL messages to it (except escape to exit)
	if m.findReplaceMode {
		// Check for escape to exit (only in input fields state)
		if keyMsg, ok := msg.(tea.KeyMsg); ok {
			if keyMsg.String() == "esc" && m.findReplaceModel.State == findreplace.StateInputFields {
				m.findReplaceMode = false
				return m, nil
			}
		}
		// Delegate ALL messages to findreplace model
		m.findReplaceModel, cmd = m.findReplaceModel.Update(msg)
		return m, cmd
	}

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
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
				m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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

		// If in move mode, allow esc to cancel
		if m.moveMode {
			if msg.String() == "esc" {
				m.moveMode = false
				m.moveSource = ""
				return m, nil
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
				m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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

		// If in search mode, handle search input and navigation
		if m.searchMode {
			return m.handleSearchMode(msg)
		}

		// If in theme picker mode, handle theme picker input and navigation
		if m.themePickerOpen {
			return m.handleThemePickerMode(msg)
		}

		// Reset gg double-tap detection for any key except 'g' or 'G'
		keyStr := msg.String()
		if keyStr != "g" && keyStr != "G" {
			m.lastKeyWasG = false
		}

		switch keyStr {
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "F":
			// Enter find/replace mode
			m.findReplaceMode = true
			m.findReplaceModel = findreplace.New(m.rootPath, m.sessionID, m.theme.Current)
			// Return both WindowSizeMsg and Init commands
			return m, tea.Batch(
				func() tea.Msg {
					return tea.WindowSizeMsg{Width: m.width, Height: m.height}
				},
				m.findReplaceModel.Init(),
			)
		case "/":
			// Enter search mode (only if not in other modals)
			if !m.showHelp && !m.showViewer && m.creatingMode == creationNone && m.deletePending == nil {
				m.searchMode = true
				m.searchInput = textinput.New()
				m.searchInput.Placeholder = "Search files and directories..."
				m.searchInput.Focus()
				m.searchInput.CharLimit = 255
				m.searchInput.Width = 50
				m.searchResults = nil
				m.searchSelectedIdx = 0

				// Initialize search results viewport
				m.searchViewport = viewport.New(56, 12) // Width 56 (fits in modal), Height 12 lines
				m.searchViewport.SetContent("")
			}
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
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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

			// Rebuild tree with new theme colors
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
			m.updateTreeCache()

			// Update viewport with new tree
			content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(content)
			m.lastContent = content
			return m, nil
		case "T":
			// Open theme picker (only if not in other modals)
			if !m.showHelp && !m.showViewer && m.creatingMode == creationNone && m.deletePending == nil && !m.searchMode {
				m.themePickerOpen = true
				m.themeFilterInput = textinput.New()
				m.themeFilterInput.Placeholder = "Search themes..."
				m.themeFilterInput.Focus()
				m.themeFilterInput.CharLimit = 50
				m.themeFilterInput.Width = 40
				m.themeFilteredList = m.theme.AllNames
				m.themeSelectedIdx = 0
				return m, textinput.Blink
			}
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
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
		case "G":
			// Jump to bottom (vim-style)
			m.selectedLine = m.maxLine
			content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
			m.viewport.SetContent(content)
			m.viewport.GotoBottom()
			m.lastKeyWasG = false // Reset g flag
			return m, nil
		case "g":
			// Handle gg for jump to top (vim-style double-tap)
			if m.lastKeyWasG && time.Since(m.lastKeyTime) < 500*time.Millisecond {
				// Double-tap detected - jump to top
				m.selectedLine = 0
				content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(content)
				m.viewport.GotoTop()
				m.lastKeyWasG = false
			} else {
				// First g press - set flag and wait for second
				m.lastKeyWasG = true
				m.lastKeyTime = time.Now()
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
					m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
			m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
					m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
					m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
			// If in move mode, complete the move
			if m.moveMode {
				// Get destination directory
				var destDir string
				if dirPath, ok := m.dirMap[m.selectedLine]; ok {
					destDir = filepath.Join(m.rootPath, dirPath)
				} else {
					// Not on a directory, can't drop here
					return m, nil
				}

				// Perform the move
				err := internal.MoveFileOrFolder(m.moveSource, destDir)
				if err != nil {
					// TODO: Show error to user
					// For now, just cancel move mode
					m.moveMode = false
					m.moveSource = ""
					return m, nil
				}

				// Move successful - exit move mode and rebuild tree
				m.moveMode = false
				m.moveSource = ""

				// Rebuild tree to reflect changes
				m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
				m.updateTreeCache()
				content := renderTreeWithSelection(m.treeString, m.selectedLine)
				m.viewport.SetContent(content)
				m.lastContent = content

				return m, nil
			}

			// Normal enter behavior - select file for viewer
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
		case "m":
			// Start move mode - grab current file or folder
			if !m.moveMode {
				var sourcePath string

				// Check if selected line is a directory
				if dirPath, ok := m.dirMap[m.selectedLine]; ok {
					sourcePath = filepath.Join(m.rootPath, dirPath)
				} else if filePath, ok := m.fileMap[m.selectedLine]; ok {
					sourcePath = filepath.Join(m.rootPath, filePath)
				} else {
					// Nothing selected
					return m, nil
				}

				m.moveMode = true
				m.moveSource = sourcePath
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
		m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
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
			BorderForeground(m.theme.Current.Magenta)

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
			BorderForeground(m.theme.Current.BrightGreen)

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
			BorderForeground(m.theme.Current.Magenta)

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
			BorderForeground(m.theme.Current.BrightRed) // Red for danger

		return lipgloss.Place(
			m.width,
			m.height,
			lipgloss.Center,
			lipgloss.Center,
			confirmStyle.Render(confirmText),
		)
	}

	// Show find/replace mode
	if m.findReplaceMode {
		return m.findReplaceModel.View()
	}

	// Show search modal
	if m.searchMode {
		return m.renderSearchModal()
	}

	// Show theme picker modal
	if m.themePickerOpen {
		return m.renderThemePicker()
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
  g g           Jump to top
  G             Jump to bottom
  /             Fuzzy search
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
  m             Move file/folder
  v             Show viewer command
  ?             Toggle this help
  q             Quit

Git Features
────────────
  • Shows uncommitted changes (+N)
  • Works without remote repos
  • Auto-creates GitHub repos

Symlinks
────────
  • Symlinks shown with → indicator
  • Cyan color for symlinks
  • Navigate symlinked dirs like normal
  • Broken symlinks shown in red
  • Loop detection prevents hangs

Press any key to dismiss...`

		helpStyle := lipgloss.NewStyle().
			Padding(2, 4).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.theme.Current.Magenta)

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
			Foreground(m.theme.Current.BrightGreen). // Green
			Bold(true)
		hint := copyHintStyle.Render(fmt.Sprintf(" [Copied: %s]", m.copiedPath))
		title = title + hint
	}

	// Add move mode indicator if active
	if m.moveMode {
		moveHintStyle := lipgloss.NewStyle().
			Foreground(m.theme.Current.BrightYellow). // Yellow for move
			Bold(true)
		itemName := filepath.Base(m.moveSource)
		hint := moveHintStyle.Render(fmt.Sprintf(" [Moving: %s]", itemName))
		title = title + hint
	}

	// Use theme colors for header
	themedHeaderStyle := m.theme.CreateHeaderStyle()
	return themedHeaderStyle.Width(m.width).Render(title)
}

func (m model) footerView() string {
	// Minimal footer - just the essentials
	var info string
	if m.moveMode {
		// Show move mode hints
		itemName := filepath.Base(m.moveSource)
		info = fmt.Sprintf("j/k: nav | enter: drop here | esc: cancel | Moving: %s", itemName)
	} else {
		info = fmt.Sprintf("j/k: nav | space: select | t/T: theme [%s] | F: find/replace | ?: help | q: quit", m.theme.Current.Name)
	}
	footerStyle := m.theme.CreateHeaderStyle()
	return footerStyle.Width(m.width).Render(info)
}

// handleSearchMode handles all search mode interactions
func (m model) handleSearchMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			// Exit search mode
			m.searchMode = false
			m.searchInput.Reset()
			m.searchResults = nil
			m.searchSelectedIdx = 0
			return m, nil
		case "enter":
			// Jump to selected result
			if len(m.searchResults) > 0 && m.searchSelectedIdx < len(m.searchResults) {
				result := m.searchResults[m.searchSelectedIdx]

				// If nesting is disabled, expand all parent directories
				if !m.nestingEnabled {
					// Get all parent directories
					pathParts := strings.Split(result.path, string(filepath.Separator))
					currentPath := ""
					for i := 0; i < len(pathParts)-1; i++ {
						if currentPath == "" {
							currentPath = pathParts[i]
						} else {
							currentPath = filepath.Join(currentPath, pathParts[i])
						}
						m.expandedDirs[currentPath] = true
					}
				}

				// Rebuild tree with expanded directories
				m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
				m.updateTreeCache()

				// Find the line number for the selected path
				selectedLine := 0
				if result.isDir {
					for line, path := range m.dirMap {
						if path == result.path {
							selectedLine = line
							break
						}
					}
				} else {
					for line, path := range m.fileMap {
						if path == result.path {
							selectedLine = line
							break
						}
					}
				}

				m.selectedLine = selectedLine

				// Update viewport with new selection
				content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(content)

				// Auto-scroll to selection
				if m.selectedLine < m.viewport.YOffset {
					m.viewport.GotoTop()
					for i := 0; i < m.selectedLine; i++ {
						m.viewport.LineDown(1)
					}
				} else if m.selectedLine >= m.viewport.YOffset+m.viewport.Height-1 {
					m.viewport.GotoTop()
					for i := 0; i < m.selectedLine; i++ {
						m.viewport.LineDown(1)
					}
				}

				// Exit search mode
				m.searchMode = false
				m.searchInput.Reset()
				m.searchResults = nil
				m.searchSelectedIdx = 0
			}
			return m, nil
		case "down", "j":
			// Navigate down in results
			if len(m.searchResults) > 0 && m.searchSelectedIdx < len(m.searchResults)-1 {
				m.searchSelectedIdx++
				// Update viewport content with new selection
				m.updateSearchViewport()
			}
			return m, nil
		case "up", "k":
			// Navigate up in results
			if m.searchSelectedIdx > 0 {
				m.searchSelectedIdx--
				// Update viewport content with new selection
				m.updateSearchViewport()
			}
			return m, nil
		case "pgdown":
			// Page down in results
			if len(m.searchResults) > 0 {
				m.searchSelectedIdx += m.searchViewport.Height
				if m.searchSelectedIdx >= len(m.searchResults) {
					m.searchSelectedIdx = len(m.searchResults) - 1
				}
				m.updateSearchViewport()
			}
			return m, nil
		case "pgup":
			// Page up in results
			if m.searchSelectedIdx > 0 {
				m.searchSelectedIdx -= m.searchViewport.Height
				if m.searchSelectedIdx < 0 {
					m.searchSelectedIdx = 0
				}
				m.updateSearchViewport()
			}
			return m, nil
		default:
			// Handle text input
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)

			// Perform search on input change
			query := strings.TrimSpace(m.searchInput.Value())
			if query != "" {
				m.searchResults = m.performSearch(query)
				m.searchSelectedIdx = 0
				// Update viewport with new results
				m.updateSearchViewport()
			} else {
				m.searchResults = nil
				m.searchSelectedIdx = 0
				m.searchViewport.SetContent("")
			}

			return m, cmd
		}
	}
	return m, nil
}

// handleThemePickerMode handles all theme picker interactions
func (m model) handleThemePickerMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			// Exit theme picker
			m.themePickerOpen = false
			m.themeFilterInput.Reset()
			m.themeFilteredList = nil
			m.themeSelectedIdx = 0
			return m, nil
		case "enter":
			// Select theme
			if len(m.themeFilteredList) > 0 && m.themeSelectedIdx < len(m.themeFilteredList) {
				selectedTheme := m.themeFilteredList[m.themeSelectedIdx]
				m.theme.SelectTheme(selectedTheme)

				// Rebuild tree with new theme colors
				m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(m.rootPath, m.diffCache, m.gitignore, m.respectIgnore, m.nestingEnabled, m.expandedDirs, m.showHidden, m.theme.Current)
				m.updateTreeCache()

				// Update viewport with new tree
				content := renderTreeWithSelectionOptimized(m.treeLines, m.selectedLine)
				m.viewport.SetContent(content)
				m.lastContent = content
			}
			// Exit theme picker
			m.themePickerOpen = false
			m.themeFilterInput.Reset()
			m.themeFilteredList = nil
			m.themeSelectedIdx = 0
			return m, nil
		case "down", "j":
			// Navigate down in list
			if len(m.themeFilteredList) > 0 && m.themeSelectedIdx < len(m.themeFilteredList)-1 {
				m.themeSelectedIdx++
			}
			return m, nil
		case "up", "k":
			// Navigate up in list
			if m.themeSelectedIdx > 0 {
				m.themeSelectedIdx--
			}
			return m, nil
		case "pgdown":
			// Page down (jump 10)
			if len(m.themeFilteredList) > 0 {
				m.themeSelectedIdx += 10
				if m.themeSelectedIdx >= len(m.themeFilteredList) {
					m.themeSelectedIdx = len(m.themeFilteredList) - 1
				}
			}
			return m, nil
		case "pgup":
			// Page up (jump 10)
			m.themeSelectedIdx -= 10
			if m.themeSelectedIdx < 0 {
				m.themeSelectedIdx = 0
			}
			return m, nil
		default:
			// Update filter input
			var cmd tea.Cmd
			m.themeFilterInput, cmd = m.themeFilterInput.Update(msg)

			// Filter themes on input change
			query := strings.ToLower(strings.TrimSpace(m.themeFilterInput.Value()))
			if query != "" {
				// Simple substring matching
				m.themeFilteredList = nil
				for _, name := range m.theme.AllNames {
					if strings.Contains(strings.ToLower(name), query) {
						m.themeFilteredList = append(m.themeFilteredList, name)
					}
				}
				m.themeSelectedIdx = 0
			} else {
				m.themeFilteredList = m.theme.AllNames
				m.themeSelectedIdx = 0
			}

			return m, cmd
		}
	}
	return m, nil
}

// updateSearchViewport updates the search viewport content with current results and selection
func (m *model) updateSearchViewport() {
	if len(m.searchResults) == 0 {
		m.searchViewport.SetContent("")
		return
	}

	var lines []string
	for i, result := range m.searchResults {
		// Highlight selected result
		style := lipgloss.NewStyle()
		if i == m.searchSelectedIdx {
			style = style.Reverse(true).Bold(true)
		}

		// Show directory indicator
		prefix := "  "
		if result.isDir {
			prefix = "📁"
		}

		line := fmt.Sprintf("%s %s", prefix, result.path)
		lines = append(lines, style.Render(line))
	}

	content := strings.Join(lines, "\n")
	m.searchViewport.SetContent(content)

	// Auto-scroll to keep selected item visible
	// Calculate which line the selected item is on
	if m.searchSelectedIdx < m.searchViewport.YOffset {
		// Selected item is above viewport, scroll up
		m.searchViewport.YOffset = m.searchSelectedIdx
	} else if m.searchSelectedIdx >= m.searchViewport.YOffset+m.searchViewport.Height {
		// Selected item is below viewport, scroll down
		m.searchViewport.YOffset = m.searchSelectedIdx - m.searchViewport.Height + 1
	}
}

// getAllPaths recursively collects all file and directory paths regardless of expansion state
func (m *model) getAllPaths() []searchResult {
	var results []searchResult
	visited := newVisitedPaths() // Track visited paths for symlink loop detection

	// Helper to recursively walk directories
	var walk func(relPath string)
	walk = func(relPath string) {
		fullPath := filepath.Join(m.rootPath, relPath)

		// Check for symlink loops
		if !visited.visit(fullPath) {
			return // Loop detected, skip
		}

		entries, err := os.ReadDir(fullPath)
		if err != nil {
			return
		}

		for _, entry := range entries {
			name := entry.Name()

			// Skip hidden files if not showing hidden
			if !m.showHidden && strings.HasPrefix(name, ".") {
				continue
			}

			itemRelPath := name
			if relPath != "" {
				itemRelPath = filepath.Join(relPath, name)
			}

			itemFullPath := filepath.Join(m.rootPath, itemRelPath)

			// Check gitignore
			if m.respectIgnore && m.gitignore != nil && m.gitignore.IsIgnored(itemFullPath) {
				continue
			}

			isDir := entry.IsDir()

			// Handle symlinks
			if isSymlink(entry) {
				symlinkPath := itemFullPath
				isDirTarget, isBroken, err := isSymlinkToDir(symlinkPath)
				if err != nil || isBroken {
					// Skip broken symlinks
					continue
				}
				isDir = isDirTarget
			}

			// Add to results
			results = append(results, searchResult{
				path:  itemRelPath,
				isDir: isDir,
			})

			// Recurse into directories
			if isDir {
				walk(itemRelPath)
			}
		}
	}

	walk("")
	return results
}

// performSearch performs fuzzy search on all files and directories
func (m *model) performSearch(query string) []searchResult {
	if query == "" {
		return nil
	}

	// Get all paths from filesystem (ignores expansion state)
	allPaths := m.getAllPaths()

	// Build string slice for fuzzy matching
	pathStrings := make([]string, len(allPaths))
	for i, item := range allPaths {
		pathStrings[i] = item.path
	}

	// Perform fuzzy search
	matches := fuzzy.Find(query, pathStrings)

	// Convert to search results (limit to 50 for performance)
	maxResults := 50
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	results := make([]searchResult, len(matches))
	for i, match := range matches {
		item := allPaths[match.Index]
		results[i] = searchResult{
			path:       item.path,
			matchScore: match.Score,
			isDir:      item.isDir,
			lineNum:    -1, // Will be set after tree expansion
		}
	}

	return results
}

// renderSearchModal renders the search modal overlay
func (m *model) renderSearchModal() string {
	// Build search results display
	var resultsDisplay string
	if len(m.searchResults) == 0 {
		if m.searchInput.Value() == "" {
			resultsDisplay = lipgloss.NewStyle().
				Foreground(m.theme.Current.BrightBlack).
				Render("Type to search...")
		} else {
			resultsDisplay = lipgloss.NewStyle().
				Foreground(m.theme.Current.BrightBlack).
				Render("No matches found")
		}
	} else {
		// Use viewport for scrollable results
		resultsDisplay = m.searchViewport.View()
	}

	// Build status line with result count
	var statusLine string
	if len(m.searchResults) > 0 {
		statusLine = lipgloss.NewStyle().
			Foreground(m.theme.Current.BrightBlack).
			Render(fmt.Sprintf("Showing %d of %d results | ↑↓ j/k navigate | PgUp/PgDn scroll | Enter select | Esc cancel",
				m.searchSelectedIdx+1, len(m.searchResults)))
	} else {
		statusLine = lipgloss.NewStyle().
			Foreground(m.theme.Current.BrightBlack).
			Render("↑↓ j/k navigate | Enter select | Esc cancel")
	}

	// Build full modal
	modalContent := fmt.Sprintf(
		"Search: %s\n\n%s\n\n%s",
		m.searchInput.View(),
		resultsDisplay,
		statusLine,
	)

	modalStyle := lipgloss.NewStyle().
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Current.Magenta).
		Width(60)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		modalStyle.Render(modalContent),
	)
}

// renderThemePicker renders the theme picker modal overlay
func (m *model) renderThemePicker() string {
	// Build theme list display
	var listDisplay string
	if len(m.themeFilteredList) == 0 {
		listDisplay = lipgloss.NewStyle().
			Foreground(m.theme.Current.BrightBlack).
			Render("No themes match filter")
	} else {
		// Show max 15 themes at a time
		maxDisplay := 15
		startIdx := m.themeSelectedIdx
		if startIdx > len(m.themeFilteredList)-maxDisplay {
			startIdx = len(m.themeFilteredList) - maxDisplay
		}
		if startIdx < 0 {
			startIdx = 0
		}

		var lines []string
		for i := startIdx; i < len(m.themeFilteredList) && i < startIdx+maxDisplay; i++ {
			themeName := m.themeFilteredList[i]
			if i == m.themeSelectedIdx {
				// Selected theme
				style := lipgloss.NewStyle().
					Foreground(m.theme.Current.BrightGreen).
					Bold(true)
				lines = append(lines, style.Render("> "+themeName))
			} else {
				// Normal theme
				style := lipgloss.NewStyle().
					Foreground(m.theme.Current.Foreground)
				lines = append(lines, style.Render("  "+themeName))
			}
		}
		listDisplay = strings.Join(lines, "\n")
	}

	// Build status line
	var statusLine string
	if len(m.themeFilteredList) > 0 {
		statusLine = lipgloss.NewStyle().
			Foreground(m.theme.Current.BrightBlack).
			Render(fmt.Sprintf("Showing %d of %d themes | ↑↓ j/k navigate | PgUp/PgDn jump | Enter select | Esc cancel",
				m.themeSelectedIdx+1, len(m.themeFilteredList)))
	} else {
		statusLine = lipgloss.NewStyle().
			Foreground(m.theme.Current.BrightBlack).
			Render("Type to filter themes | Esc cancel")
	}

	// Build full modal
	modalContent := fmt.Sprintf(
		"Theme Picker: %s\n\n%s\n\n%s",
		m.themeFilterInput.View(),
		listDisplay,
		statusLine,
	)

	modalStyle := lipgloss.NewStyle().
		Padding(2, 4).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.Current.Magenta).
		Width(60)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		modalStyle.Render(modalContent),
	)
}

func tick() tea.Cmd {
	// Reduced frequency: manual refresh with 'r' key is preferred for performance
	return tea.Tick(60*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// buildTree recursively builds a file tree with git diff tracking
func buildTree(rootPath string, theme lipglossthemes.Theme) *tree.Tree {
	return buildTreeRecursive(rootPath, "", nil, nil, false, theme)
}

// buildTreeWithCache builds a file tree using cached git diff data
func buildTreeWithCache(rootPath string, diffCache map[string]int, theme lipglossthemes.Theme) *tree.Tree {
	return buildTreeRecursive(rootPath, "", diffCache, nil, false, theme)
}

// buildTreeWithOptions builds a file tree with all options
func buildTreeWithOptions(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, theme lipglossthemes.Theme) *tree.Tree {
	return buildTreeRecursive(rootPath, "", diffCache, gitignore, respectIgnore, theme)
}

// buildTreeWithMap builds tree and returns a map of line numbers to file paths (deprecated, use buildTreeWithMaps)
func buildTreeWithMap(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, nestingEnabled bool, theme lipglossthemes.Theme) (*tree.Tree, map[int]string) {
	fileMap := make(map[int]string)
	lineNum := 1                 // Start at 1 because the root directory takes line 0
	visited := newVisitedPaths() // Track visited paths for symlink loop detection
	t := buildTreeRecursiveWithMap(rootPath, "", diffCache, gitignore, respectIgnore, nestingEnabled, make(map[string]bool), false, &lineNum, fileMap, nil, visited, 0, theme)
	return t, fileMap
}

// buildTreeWithMaps builds tree and returns maps of line numbers to file paths and directory paths
func buildTreeWithMaps(rootPath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, nestingEnabled bool, expandedDirs map[string]bool, showHidden bool, theme lipglossthemes.Theme) (*tree.Tree, map[int]string, map[int]string) {
	fileMap := make(map[int]string)
	dirMap := make(map[int]string)
	lineNum := 1                 // Start at 1 because the root directory takes line 0
	visited := newVisitedPaths() // Track visited paths for symlink loop detection
	t := buildTreeRecursiveWithMap(rootPath, "", diffCache, gitignore, respectIgnore, nestingEnabled, expandedDirs, showHidden, &lineNum, fileMap, dirMap, visited, 0, theme)
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

func buildTreeRecursiveWithMap(path string, relativePath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, nestingEnabled bool, expandedDirs map[string]bool, showHidden bool, lineNum *int, fileMap map[int]string, dirMap map[int]string, visited *visitedPaths, depth int, theme lipglossthemes.Theme) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(theme.BrightBlack))

	// Check max depth (prevent extremely deep symlink chains)
	const maxDepth = 10
	if depth > maxDepth {
		warningStyle := lipgloss.NewStyle().Foreground(theme.BrightYellow)
		t.Child(warningStyle.Render("⚠ Max depth reached"))
		return t
	}

	// Check for loops
	if !visited.visit(path) {
		// Loop detected
		warningStyle := lipgloss.NewStyle().Foreground(theme.BrightYellow)
		t.Child(warningStyle.Render("⚠ Symlink loop detected"))
		return t
	}

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

		// Check if this is a symlink
		isSymlinkEntry := isSymlink(entry)

		if isSymlinkEntry {
			// Handle symlinks specially
			targetIsDir, isBroken, err := isSymlinkToDir(fullPath)

			if isBroken || err != nil {
				// Broken symlink - show in red
				brokenStyle := lipgloss.NewStyle().Foreground(theme.BrightRed)
				displayName := entryName + " → (broken)"
				t.Child(brokenStyle.Render(displayName))
				*lineNum++
				continue
			}

			// Get symlink target for display
			targetPath, _ := getSymlinkTarget(fullPath)
			symlinkStyle := lipgloss.NewStyle().Foreground(theme.Cyan)

			if targetIsDir {
				// Symlinked directory
				displayName := entryName + " → " + targetPath + "/"

				// Track in dirMap
				if dirMap != nil {
					dirMap[*lineNum] = relPath
				}
				*lineNum++

				// Allow expansion like normal directories
				shouldExpand := nestingEnabled || (expandedDirs != nil && expandedDirs[relPath])

				if shouldExpand {
					// Recursively build (with loop protection and increased depth)
					subTree := buildTreeRecursiveWithMap(
						fullPath, relPath, diffCache, gitignore,
						respectIgnore, nestingEnabled, expandedDirs,
						showHidden, lineNum, fileMap, dirMap, visited, depth+1, theme,
					)
					// Style the root with symlink indicator
					styledRoot := symlinkStyle.Render(displayName)
					subTree = tree.Root(styledRoot).
						EnumeratorStyle(lipgloss.NewStyle().Foreground(theme.BrightBlack))

					// Re-scan and add children
					subEntries, err := os.ReadDir(fullPath)
					if err == nil {
						for _, subEntry := range subEntries {
							subFullPath := filepath.Join(fullPath, subEntry.Name())
							subRelPath := filepath.Join(relPath, subEntry.Name())

							if subEntry.Name() == ".git" {
								continue
							}

							subIsHidden := strings.HasPrefix(subEntry.Name(), ".")
							if subIsHidden && subEntry.Name() != ".gitignore" && !showHidden {
								continue
							}

							if respectIgnore && gitignore != nil && gitignore.IsIgnored(subFullPath) {
								continue
							}

							if subEntry.IsDir() || (isSymlink(subEntry) && func() bool { isDir, _, _ := isSymlinkToDir(subFullPath); return isDir }()) {
								subTreeChild := buildTreeRecursiveWithMap(
									subFullPath, subRelPath, diffCache, gitignore,
									respectIgnore, nestingEnabled, expandedDirs,
									showHidden, lineNum, fileMap, dirMap, visited, depth+1, theme,
								)
								subTree.Child(subTreeChild)
							} else {
								// File handling
								fileMap[*lineNum] = subRelPath
								*lineNum++

								var diffLines int
								if diffCache != nil {
									diffLines = diffCache[subRelPath]
								}

								fileStyle := lipgloss.NewStyle().Foreground(theme.Foreground)
								name := fileStyle.Render(subEntry.Name())

								if diffLines > 0 {
									diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen)
									name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
								} else if diffLines == -1 {
									diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen)
									name = name + diffStyle.Render(" (new)")
								}

								subTree.Child(name)
							}
						}
					}
					t.Child(subTree)
				} else {
					// Collapsed symlinked directory
					t.Child(symlinkStyle.Render(displayName))
				}
			} else {
				// Symlinked file
				displayName := entryName + " → " + targetPath
				fileMap[*lineNum] = relPath
				*lineNum++

				// Check for git diff on symlinked file
				var diffLines int
				if diffCache != nil {
					diffLines = diffCache[relPath]
				}

				name := symlinkStyle.Render(displayName)
				if diffLines > 0 {
					diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen)
					name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
				} else if diffLines == -1 {
					diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen)
					name = name + diffStyle.Render(" (new)")
				}

				t.Child(name)
			}
			continue
		}

		// Regular file or directory (not a symlink)
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
				subTree := buildTreeRecursiveWithMap(fullPath, relPath, diffCache, gitignore, respectIgnore, nestingEnabled, expandedDirs, showHidden, lineNum, fileMap, dirMap, visited, depth+1, theme)
				t.Child(subTree)
			} else {
				// Show collapsed directory (including hidden directories when showHidden is true)
				dirStyle := lipgloss.NewStyle().Foreground(theme.BrightBlue)
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
			fileStyle := lipgloss.NewStyle().Foreground(theme.Foreground)
			name := fileStyle.Render(entryName)

			// Add diff indicator if file has changes
			if diffLines > 0 {
				diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen) // Green
				name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
			} else if diffLines == -1 {
				// New untracked file (marked as -1 to avoid expensive line counting)
				diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen) // Green
				name = name + diffStyle.Render(" (new)")
			}

			t.Child(name)
		}
	}

	return t
}

func buildTreeRecursive(path string, relativePath string, diffCache map[string]int, gitignore *internal.GitIgnore, respectIgnore bool, theme lipglossthemes.Theme) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(theme.BrightBlack))

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
			subTree := buildTreeRecursive(fullPath, relPath, diffCache, gitignore, respectIgnore, theme)
			t.Child(subTree)
		} else {
			// Get git diff lines from cache
			var diffLines int
			if diffCache != nil {
				diffLines = diffCache[relPath]
			}

			// Style filename (including hidden files when showHidden is true)
			fileStyle := lipgloss.NewStyle().Foreground(theme.Foreground)
			name := fileStyle.Render(entryName)

			// Add diff indicator if file has changes
			if diffLines > 0 {
				diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen) // Green
				name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
			} else if diffLines == -1 {
				// New untracked file (marked as -1 to avoid expensive line counting)
				diffStyle := lipgloss.NewStyle().Foreground(theme.BrightGreen) // Green
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

	// Initialize theme manager with session
	themeManager := internal.NewThemeManagerWithSession(sessionID)

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
		// Get default theme for benchmark
		defaultTheme, _ := lipglossthemes.Get("Dracula")

		for i := 0; i < 3; i++ {
			start = time.Now()
			_, _, _ = buildTreeWithMaps(watchPath, diffCache, gitignore, true, false, make(map[string]bool), false, defaultTheme)
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
	showHidden := false     // Hidden files/folders off by default
	expandedDirs := make(map[string]bool)
	tree, fileMap, dirMap := buildTreeWithMaps(watchPath, initialDiffCache, gitignore, respectIgnore, nestingEnabled, expandedDirs, showHidden, themeManager.Current)

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
