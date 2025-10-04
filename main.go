package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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

// Model
type model struct {
	rootPath string
	tree     *tree.Tree
	viewport viewport.Model
	ready    bool
	width    int
	height   int
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
			if m.tree != nil {
				m.viewport.SetContent(m.tree.String())
			}
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMargins
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		}

	case tickMsg:
		// Rebuild tree
		m.tree = buildTree(m.rootPath)
		m.viewport.SetContent(m.tree.String())
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
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func (m model) headerView() string {
	title := fmt.Sprintf("Vinw - %s", m.rootPath)
	return headerStyle.Width(m.width).Render(title)
}

func (m model) footerView() string {
	info := "↑/↓: scroll | q: quit"
	return footerStyle.Width(m.width).Render(info)
}

func tick() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// buildTree recursively builds a file tree with git diff tracking
func buildTree(rootPath string) *tree.Tree {
	return buildTreeRecursive(rootPath, "")
}

func buildTreeRecursive(path string, relativePath string) *tree.Tree {
	dirName := filepath.Base(path)
	t := tree.Root(dirName)

	entries, err := os.ReadDir(path)
	if err != nil {
		return t
	}

	for _, entry := range entries {
		// Skip hidden files
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		fullPath := filepath.Join(path, entry.Name())
		relPath := filepath.Join(relativePath, entry.Name())

		if entry.IsDir() {
			// Skip .git directory
			if entry.Name() == ".git" {
				continue
			}
			// Recursively build subtree
			subTree := buildTreeRecursive(fullPath, relPath)
			t.Child(subTree)
		} else {
			// Get git diff lines
			diffLines := getGitDiffLines(fullPath)

			// Normal style for filename
			fileStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
			name := fileStyle.Render(entry.Name())

			// Add diff indicator if file has changes
			if diffLines > 0 {
				diffStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
				name = name + diffStyle.Render(fmt.Sprintf(" (+%d)", diffLines))
			}

			t.Child(name)
		}
	}

	return t
}

func main() {
	// Get watch path from args or use current directory
	watchPath := "."
	if len(os.Args) > 1 {
		watchPath = os.Args[1]
	}

	// Get absolute path for consistent Skate keys
	absPath, _ := filepath.Abs(watchPath)

	// Initialize GitHub repo if needed (only on first run for this directory)
	if err := initGitHub(absPath); err != nil {
		fmt.Printf("Error: %v\n", err)
	}

	// Build initial tree
	tree := buildTree(watchPath)

	// Initialize model
	m := model{
		rootPath: watchPath,
		tree:     tree,
	}

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
