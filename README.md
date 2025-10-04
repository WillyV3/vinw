# <div align="center">vinw</div>

<div align="center">

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev/)
[![Bubble Tea](https://img.shields.io/badge/Bubble%20Tea-TUI-FF6B9D?style=flat)](https://github.com/charmbracelet/bubbletea)
[![GitHub CLI](https://img.shields.io/badge/GitHub%20CLI-Required-181717?style=flat&logo=github)](https://cli.github.com/)

</div>

A file tree viewer that tracks git changes in real-time.

## Features

- Real-time git diff tracking with line count indicators
- GitHub repository creation from the command line
- Gitignore toggle to show/hide ignored files
- Smooth scrolling for large repositories
- Automatic detection of broken remote repositories

## Installation

```bash
go build -o vinw
```

## Usage

### File Tree Viewer
```bash
# View current directory
./vinw

# View specific directory
./vinw /path/to/directory
```

### File Content Viewer (Terminal)
```bash
# In another terminal, start the viewer
./vinw-viewer

# The viewer will automatically show the selected file
# Updates when you select a new file in vinw
```

## Controls

### vinw (File Tree)
- `↑/↓` - Scroll through the file tree
- `i` - Toggle gitignore (show/hide ignored files)
- `Enter` - Select current file for viewing in server
- `q` - Quit

### vinw-viewer (File Viewer)
- `↑/↓` or mouse scroll - Scroll through file
- `r` - Manual refresh
- `q` - Quit
- Auto-refreshes every second
- Shows line numbers for code files
- Displays scroll position and line count

## Requirements

- Go 1.21+
- GitHub CLI (for repository creation)
- Git

## How It Works

vinw displays a file tree with git diff information, showing `(+N)` next to files with uncommitted changes. The tree updates every 5 seconds to reflect new changes.

When run in a directory without a git repository, vinw offers to create a GitHub repository using the GitHub CLI. It supports multiple GitHub accounts and organizations.

If a local repository's remote has been deleted, vinw detects this and offers to create a new remote repository while preserving local commits.