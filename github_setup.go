package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type setupStep int

const (
	stepSelectAccount setupStep = iota
	stepConfirmCreate
	stepEnterName
	stepEnterDescription
	stepSelectVisibility
	stepCreating
	stepDone
	stepDeclined
)

type githubSetupModel struct {
	step        setupStep
	accounts    []string
	selected    int
	account     string
	repoName    textinput.Model
	description textinput.Model
	isPublic    bool
	path        string
	err         error
	width       int
	height      int
}

var (
	titleStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("62")).
		Bold(true)

	selectedStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("42")).
		Bold(true)

	promptStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("241"))

	errorStyle = lipgloss.NewStyle().
		Foreground(lipgloss.Color("196"))
)

func newGitHubSetupModel(path string) githubSetupModel {
	ti := textinput.New()
	ti.Placeholder = "my-awesome-project"
	ti.Focus()
	ti.CharLimit = 100
	ti.Width = 40

	desc := textinput.New()
	desc.Placeholder = "A brief description (optional)"
	desc.CharLimit = 200
	desc.Width = 60

	return githubSetupModel{
		step:        stepSelectAccount,
		accounts:    getGitHubAccounts(),
		path:        path,
		repoName:    ti,
		description: desc,
	}
}

func (m githubSetupModel) Init() tea.Cmd {
	// If only one account, skip selection
	if len(m.accounts) == 1 {
		m.account = m.accounts[0]
		m.step = stepConfirmCreate
	} else if len(m.accounts) == 0 {
		m.err = fmt.Errorf("no GitHub accounts found")
		m.step = stepDone
	}
	return textinput.Blink
}

func (m githubSetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch m.step {
		case stepSelectAccount:
			switch msg.String() {
			case "up", "k":
				if m.selected > 0 {
					m.selected--
				}
			case "down", "j":
				if m.selected < len(m.accounts)-1 {
					m.selected++
				}
			case "enter":
				m.account = m.accounts[m.selected]
				m.step = stepConfirmCreate
			case "q", "esc":
				m.step = stepDeclined
				return m, tea.Quit
			}

		case stepConfirmCreate:
			switch msg.String() {
			case "y", "Y":
				m.step = stepEnterName
				return m, m.repoName.Focus()
			case "n", "N", "q", "esc":
				m.step = stepDeclined
				return m, tea.Quit
			}

		case stepEnterName:
			switch msg.String() {
			case "enter":
				if m.repoName.Value() != "" {
					m.step = stepEnterDescription
					m.repoName.Blur()
					return m, m.description.Focus()
				}
			case "esc":
				m.step = stepConfirmCreate
			default:
				m.repoName, cmd = m.repoName.Update(msg)
				return m, cmd
			}

		case stepEnterDescription:
			switch msg.String() {
			case "enter":
				m.step = stepSelectVisibility
				m.description.Blur()
			case "esc":
				m.step = stepEnterName
				m.description.Blur()
				return m, m.repoName.Focus()
			default:
				m.description, cmd = m.description.Update(msg)
				return m, cmd
			}

		case stepSelectVisibility:
			switch msg.String() {
			case "p", "P":
				m.isPublic = true
				m.step = stepCreating
				return m, m.createRepo()
			case "enter":
				m.isPublic = false
				m.step = stepCreating
				return m, m.createRepo()
			case "esc":
				m.step = stepEnterDescription
				return m, m.description.Focus()
			}

		case stepDone, stepDeclined:
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m githubSetupModel) View() string {
	var s strings.Builder

	switch m.step {
	case stepSelectAccount:
		s.WriteString(titleStyle.Render("🚀 Select GitHub Account") + "\n\n")
		for i, account := range m.accounts {
			if i == m.selected {
				s.WriteString(selectedStyle.Render("→ " + account))
			} else {
				s.WriteString("  " + account)
			}
			s.WriteString("\n")
		}
		s.WriteString("\n" + promptStyle.Render("↑/↓: select • enter: confirm • q: cancel"))

	case stepConfirmCreate:
		s.WriteString(titleStyle.Render("📁 No git repository detected") + "\n\n")
		s.WriteString(fmt.Sprintf("GitHub account: %s\n", selectedStyle.Render(m.account)))
		s.WriteString(fmt.Sprintf("Directory: %s\n\n", m.path))
		s.WriteString("Create GitHub repository to track changes?\n\n")
		s.WriteString(promptStyle.Render("y: yes • n: no"))

	case stepEnterName:
		s.WriteString(titleStyle.Render("Repository Name") + "\n\n")
		s.WriteString(m.repoName.View() + "\n\n")
		s.WriteString(promptStyle.Render("enter: continue • esc: back"))

	case stepEnterDescription:
		s.WriteString(titleStyle.Render("Repository Description") + "\n\n")
		s.WriteString(m.description.View() + "\n\n")
		s.WriteString(promptStyle.Render("enter: continue • esc: back"))

	case stepSelectVisibility:
		s.WriteString(titleStyle.Render("Repository Visibility") + "\n\n")
		s.WriteString("Select visibility:\n\n")
		s.WriteString("  [P]ublic  - Anyone can see this repository\n")
		s.WriteString("  [Enter]   - Private (default)\n\n")
		s.WriteString(promptStyle.Render("p: public • enter: private • esc: back"))

	case stepCreating:
		s.WriteString(titleStyle.Render("Creating Repository...") + "\n\n")
		s.WriteString("Setting up " + m.repoName.Value() + "...")

	case stepDone:
		if m.err != nil {
			s.WriteString(errorStyle.Render("Error: " + m.err.Error()))
		} else {
			s.WriteString(selectedStyle.Render("✅ Repository created successfully!"))
		}

	case stepDeclined:
		s.WriteString("Continuing without git tracking.\n")
		s.WriteString(promptStyle.Render("Run 'git init' manually to enable change tracking."))
	}

	return s.String()
}

func (m *githubSetupModel) createRepo() tea.Cmd {
	return func() tea.Msg {
		// Initialize git repo
		exec.Command("git", "init").Run()

		// Create GitHub repo
		args := []string{"repo", "create", m.repoName.Value()}
		if m.isPublic {
			args = append(args, "--public")
		} else {
			args = append(args, "--private")
		}
		if desc := m.description.Value(); desc != "" {
			args = append(args, "--description", desc)
		}
		args = append(args, "--source", ".")

		cmd := exec.Command("gh", args...)
		if err := cmd.Run(); err != nil {
			m.err = err
			m.step = stepDone
			return tea.Quit
		}

		// Make initial commit
		exec.Command("git", "add", ".").Run()
		exec.Command("git", "commit", "-m", "Initial commit").Run()
		exec.Command("git", "push", "-u", "origin", "main").Run()

		// Clear any previous decline
		clearRepoDeclined(m.path)

		m.step = stepDone
		return tea.Quit
	}
}

// getGitHubAccounts returns all GitHub accounts (including orgs)
func getGitHubAccounts() []string {
	var accounts []string

	// Get primary account
	cmd := exec.Command("gh", "auth", "status")
	output, err := cmd.Output()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines {
			if strings.Contains(line, "account") && strings.Contains(line, "github.com") {
				parts := strings.Fields(line)
				for i, part := range parts {
					if part == "account" && i+1 < len(parts) {
						account := parts[i+1]
						account = strings.TrimPrefix(account, "(")
						account = strings.TrimSuffix(account, ")")
						accounts = append(accounts, account)
						break
					}
				}
			}
		}
	}

	// Get organizations
	cmd = exec.Command("gh", "api", "user/orgs", "--jq", ".[].login")
	if output, err := cmd.Output(); err == nil {
		orgs := strings.Split(strings.TrimSpace(string(output)), "\n")
		for _, org := range orgs {
			if org != "" {
				accounts = append(accounts, org)
			}
		}
	}

	return accounts
}

// runGitHubSetup runs the interactive GitHub setup
func runGitHubSetup(path string) error {
	model := newGitHubSetupModel(path)
	p := tea.NewProgram(model)

	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	// Check if user declined
	if setup, ok := finalModel.(githubSetupModel); ok {
		if setup.step == stepDeclined {
			markRepoDeclined(path)
		}
		if setup.err != nil {
			return setup.err
		}
	}

	return nil
}