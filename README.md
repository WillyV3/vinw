# Vinw - Minimal File Watcher TUI

Ultra-minimal file tree watcher with real-time change detection. Built with the Vinay philosophy: simple, functional, no overengineering.

## Features

- 🌳 **Nested directory tree** - Automatic recursive display
- ✱ **Star indicator** - Green highlighting for modified files
- 📜 **Scrollable viewport** - Handle large directories with ease
- 🖱️ **Mouse support** - Scroll with mouse wheel
- 🎨 **Colored header/footer** - Info at top, controls at bottom
- 🖥️ **Fullscreen mode** - Clean, immersive interface
- ⏱️ **5-second refresh** - Auto-scan for changes
- 🚀 **202 lines total** - Minimal, readable code

## Usage

```bash
# Watch current directory
go run main.go

# Watch specific directory
go run main.go /path/to/project

# Build and run binary
go build -o vinw
./vinw
```

## Keybindings

- `↑/↓` or `k/j` - Scroll up/down
- Mouse wheel - Scroll
- `q` or `ctrl+c` - Quit

## How It Works

Uses lipgloss tree's **fluent API** for elegant recursive tree building:

```go
tree.Root("project").
    Child(
        "file1.go",
        "file2.go",
        tree.Root("subdir").Child("nested.go")
    )
```

Every 5 seconds:
1. Recursively rebuild tree
2. Check modification times vs. last scan
3. Render changed files with ✱ in green
4. Update display

## Example Output

```
┌─────────────────────────────────────────────────┐
│ Vinw - Watching: app | Changed: 1              │ (colored header)
├─────────────────────────────────────────────────┤
│                                                 │
│ app                                             │
│ ├── ✱ main.go         (green)                  │
│ ├── go.mod                                      │
│ ├── go.sum                                      │
│ └── README.md                                   │ (scrollable)
│                                                 │
│ (scroll for more...)                            │
│                                                 │
├─────────────────────────────────────────────────┤
│ Last scan: 14:23:45 | ↑/↓: scroll | q: quit    │ (footer)
└─────────────────────────────────────────────────┘
```

## Architecture

- **viewport.Model** - Scrollable container from Bubbles
- **Colored header** - Info panel with background color
- **Footer bar** - Controls and status
- **Ready pattern** - Wait for WindowSizeMsg before viewport init
- **Fluent tree API** - Elegant recursive nesting
- **tea.WithAltScreen()** - Fullscreen in one line
- **tea.WithMouseCellMotion()** - Mouse wheel support

## Dependencies

- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Bubbles](https://github.com/charmbracelet/bubbles) - Viewport component
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Styling & tree component

## Philosophy

Built following Vinay's toolsh approach:
- ✅ Minimal - Only essential features
- ✅ Functional - Does one thing well
- ✅ Beautiful - Looks cool as shit
- ✅ Simple - ~200 lines, easy to understand
- ✅ Efficient - Smart use of Charm components
