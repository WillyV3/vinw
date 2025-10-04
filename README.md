# vinw

A fast, interactive file tree viewer with real-time git change tracking and file preview.

## Features

- Live git change tracking (staged and unstaged)
- File preview in separate terminal window
- Interactive file navigation with keyboard
- Gitignore toggle to show/hide ignored files
- GitHub repository creation and management
- Optimized for large repositories

## Installation

### Quick Install
```bash
make install
```

### Manual Install
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

Start viewer in another terminal:
```bash
vinw-viewer
```

## Controls

### File Tree (vinw)
- `j/k` or `↑/↓` - Navigate files
- `Enter` - Select file for viewing
- `i` - Toggle gitignore filter
- `q` - Quit

### File Viewer (vinw-viewer)
- `↑/↓` - Scroll content
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