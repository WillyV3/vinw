# <div align="center">vinw</div>

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/Bubble%20Tea-TUI-FF6B9D?style=flat)](https://github.com/charmbracelet/bubbletea)
[![Glamour](https://img.shields.io/badge/Glamour-Markdown-9966CC?style=flat)](https://github.com/charmbracelet/glamour)
[![Homebrew](https://img.shields.io/badge/Homebrew-Install-FBB040?style=flat&logo=homebrew)](https://brew.sh/)
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat)](LICENSE)

**A fast, interactive file tree with dual-terminal viewer.**
**Everything you need to start making the switch to the terminal, without losing the convenience of a full-featured file-tree.**

<img width="2422" height="1440" alt="image" src="https://github.com/user-attachments/assets/8fe9e43c-2076-4135-b041-31ec4b6e9e47" />

</div>

"This tool is pretty sick though. you're not going to convert many emacs or vim users but I suspect there are a new crop of Claude Code-oriented devs who will give the willy stack a whirl."
-User (Staff engineer) 

## Features

### Core Features
- **Real-time git change tracking** - See exactly which files have uncommitted changes (+N indicator)
- **File & directory creation** - Create files (`a`) and directories (`A`) directly from the tree
- **Dual-terminal preview** - Separate viewer with syntax highlighting and markdown rendering
- **Session isolation** - Run multiple instances in different directories simultaneously
- **Find and Replace** - Tried to make this like VScode - Tested using property-based testing library with iver 450 different replacements.
  - Navigate to selected snippet while in Find Mode -

<img width="1958" height="693" alt="image" src="https://github.com/user-attachments/assets/04e3491a-75e6-44c9-8969-264ba0bb927e" />

**Theme Selection Menu**

<img width="559" height="513" alt="image" src="https://github.com/user-attachments/assets/7f9ded19-97e1-4f01-8c52-1b667aec39f2" />



## Installation

### Homebrew (Recommended)
```bash
brew install willyv3/tap/vinw
```

### Build from Source
```bash
make install
```

### Manual Build
```bash
go build -o vinw
cd viewer && go build -o vinw-viewer
```

### Requirements
- Go 1.21+
- Git
- [Skate](https://github.com/charmbracelet/skate) - `go install github.com/charmbracelet/skate@latest`
- GitHub CLI (optional, for repo creation)

## Usage

Start vinw in one terminal:
```bash
vinw              # Current directory
vinw /path/to/dir # Specific directory
```

vinw will display a session ID. Use it to start the viewer in another terminal:
```bash
vinw-viewer <session-id>
```

## Controls

### File Tree (vinw)

#### Navigation
- `j`/`k` or `↑`/`↓` - Navigate files and directories
- `←` - Collapse selected directory
- `→` - Expand selected directory
- `Space` or `Enter` - Select file for viewing

#### File Operations
- `a` - Create new file in current/selected directory
- `A` - Create new directory in current/selected directory
- `d` - Delete file or directory with confirmation

#### Toggles & Settings
- `h` - Toggle hidden files and folders
- `i` - Toggle gitignore filter
- `n` - Toggle directory nesting (full tree vs. collapsible)
- `t`/`T` - Cycle themes forward/backward

#### Other
- `v` - Show viewer command
- `?` - Help menu
- `q` - Quit

### File Viewer (vinw-viewer)
- `↑`/`↓` or mouse - Scroll content
- `e` - Edit file in preferred editor (nvim, vim, nano, etc.)
- `m` - Toggle mouse mode (scroll/select for copying)
- `r` - Manual refresh
- `q` - Quit

## How It Works

### Session Isolation
Each vinw instance generates a unique session ID based on the directory path. This allows you to:
- Run multiple vinw instances in different directories
- Connect the correct viewer to each instance
- Keep sessions completely isolated

### Git Integration
vinw automatically:
- Detects git repositories
- Tracks uncommitted changes (shows +N next to modified files)
- Creates GitHub repositories if they don't exist (with `gh` CLI)
- Respects `.gitignore` patterns (toggleable)

### File Creation
When you press `a` or `A`:
- A prompt appears asking for the file/directory name
- The new item is created in the currently selected directory (or parent if a file is selected)
- The tree automatically refreshes to show the new item
- Existing files/directories are protected (won't overwrite)

### File Deletion
When you press `d`:
- A confirmation prompt appears showing the file/directory to delete
- Non-empty directories display a warning with item count
- Press `y` to confirm deletion or `n`/`esc` to cancel
- The tree automatically refreshes after deletion
- This action cannot be undone - use with caution

## Testing

Run the test suite:
```bash
make test
```

## Development

```bash
make build        # Build both binaries
make clean        # Remove binaries
make run          # Run vinw
make run-viewer   # Run viewer
```

## Architecture

vinw is built completely within the Charmbracelet ecosystem:
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - Terminal UI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Styling and layout
- [Glamour](https://github.com/charmbracelet/glamour) - Markdown rendering
- [Chroma](https://github.com/alecthomas/chroma) - Syntax highlighting
- [Skate](https://github.com/charmbracelet/skate) - Session state management

## Feedback

I worked hard on vinw and the vinw-workspace companion. If you end up trying it and want to leave feedback, I'd appreciate it. I want to make it better so more people use it. This is the hard product part. Bear with me.

**Main Application (filetree and detached viewer/editor)**

```bash
brew install willyv3/tap/vinw
```
https://github.com/WillyV3/vinw

**Companion app (tmux workspace launcher)**

```bash
brew install willyv3/tap/vinw-workspace
```
https://github.com/WillyV3/vinw-workspace

**Feedback Form:** https://forms.gle/fNhsLD5tSJxL2Fu4A

-WillyV3

## License

MIT
