# <div align="center">vinw</div>

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/Bubble%20Tea-TUI-FF6B9D?style=flat)](https://github.com/charmbracelet/bubbletea)
[![Glamour](https://img.shields.io/badge/Glamour-Markdown-9966CC?style=flat)](https://github.com/charmbracelet/glamour)
[![Homebrew](https://img.shields.io/badge/Homebrew-Install-FBB040?style=flat&logo=homebrew)](https://brew.sh/)
[![License](https://img.shields.io/badge/License-MIT-blue?style=flat)](LICENSE)

**A fast, interactive file tree viewer with real-time git change tracking and syntax highlighting**

![Demo]<img width="1512" height="953" alt="Screenshot 2025-10-04 at 9 43 27 AM" src="https://github.com/user-attachments/assets/990922f7-0363-4f3a-8f35-7977e2fff8c9" />


</div>

## Features

- **Real-time git change tracking** - Shows staged, unstaged, and untracked files
- **Dual-terminal preview** - Separate viewer with syntax highlighting and markdown rendering
- **Session isolation** - Run multiple instances in different directories simultaneously
- **8 color themes** - Synchronized between tree and viewer
- **Mouse toggle** - Switch between scrolling and text selection modes
- **Vim-style navigation** - j/k keys for file tree navigation
- **Gitignore support** - Toggle with 'i' key
- **Fast performance** - Optimized for large repositories
- **GitHub integration** - Automatic repository management

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
- `j/k` or `↑/↓` - Navigate files
- `Enter` - Select file for viewing
- `i` - Toggle gitignore filter
- `t/T` - Cycle themes
- `v` - Show viewer command
- `?` - Help menu
- `q` - Quit

### File Viewer (vinw-viewer)
- `↑/↓` or mouse - Scroll content
- `m` - Toggle mouse mode (scroll/select for copying)
- `r` - Manual refresh
- `q` - Quit

## Testing

Run the test suite:
```bash
make test
```

## Development

```bash
make build    # Build both binaries
make clean    # Remove binaries
make run      # Run vinw
make run-viewer # Run viewer
```
