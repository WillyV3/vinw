# Symlink Support Plan for vinw

**Date**: October 16, 2025
**Version**: v0.6.0 (proposed)
**Status**: Planning

---

## Problem Statement

Currently, vinw does not allow users to open symlinked directories and view the files within them. When encountering a symlink to a directory, vinw treats it as a file rather than following the link to display its contents.

**Current Behavior**:
- `os.ReadDir()` returns symlinks as-is (doesn't follow them)
- `entry.IsDir()` returns `false` for symlinks to directories
- Symlinked directories appear as files in the tree

**User Impact**:
- Cannot navigate into symlinked directories
- Cannot view files within symlinked directories
- Reduces usefulness in projects with symlinked dependencies (node_modules, etc.)

---

## Research Findings (2025 Best Practices)

### Go's Symlink Behavior

1. **`os.ReadDir()`**: Does NOT follow symlinks by default
2. **`entry.IsDir()`**: Returns `false` for symlinks (even if they point to directories)
3. **`entry.Type() & os.ModeSymlink`**: Detects if entry is a symlink
4. **`os.Stat()` vs `os.Lstat()`**:
   - `os.Lstat()`: Returns info about the symlink itself (doesn't follow)
   - `os.Stat()`: Follows the symlink and returns info about the target

### Key Risks to Address

1. **Infinite loops**: Symlink cycles (A → B → A)
2. **Performance**: Following symlinks can be expensive
3. **Security**: Symlinks pointing outside repository
4. **Cross-filesystem**: Symlinks to different filesystems
5. **Broken symlinks**: Links pointing to non-existent targets

---

## Proposed Solution

### Design Philosophy

**Follow Unix/Linux file browser conventions**:
- Show symlinks with a visual indicator (→ or ⇒)
- Allow users to navigate into symlinked directories
- Detect and prevent infinite loops
- Provide clear visual feedback

### Implementation Strategy

#### 1. **Symlink Detection**

```go
func isSymlink(entry os.DirEntry) bool {
    return entry.Type()&os.ModeSymlink != 0
}

func isSymlinkToDir(fullPath string) (bool, error) {
    // Use Stat (not Lstat) to follow the link
    info, err := os.Stat(fullPath)
    if err != nil {
        return false, err // Broken symlink
    }
    return info.IsDir(), nil
}
```

#### 2. **Visual Indicators**

Add symlink styling to the tree view:

```go
// For symlinked directories
dirStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("147"))
symlinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("cyan"))

if isSymlink {
    displayName := entryName + " → " + targetPath + "/"
    dirNameStyled := symlinkStyle.Render(displayName)
}

// For symlinked files
if isSymlink {
    displayName := entryName + " → " + targetPath
    name = symlinkStyle.Render(displayName)
}
```

#### 3. **Loop Prevention**

Track visited paths to prevent infinite recursion:

```go
type visitedPaths struct {
    paths map[string]bool
    mu    sync.RWMutex
}

func (v *visitedPaths) visit(path string) bool {
    v.mu.Lock()
    defer v.mu.Unlock()

    // Resolve to canonical path
    canonical, err := filepath.EvalSymlinks(path)
    if err != nil {
        return false
    }

    if v.paths[canonical] {
        return false // Already visited (loop detected)
    }
    v.paths[canonical] = true
    return true
}
```

#### 4. **Update buildTreeRecursiveWithMap()**

```go
func buildTreeRecursiveWithMap(
    path string,
    relativePath string,
    diffCache map[string]int,
    gitignore *internal.GitIgnore,
    respectIgnore bool,
    nestingEnabled bool,
    expandedDirs map[string]bool,
    showHidden bool,
    lineNum *int,
    fileMap map[int]string,
    dirMap map[int]string,
    visited *visitedPaths, // NEW: Track visited paths
) *tree.Tree {

    // Check for loops
    if !visited.visit(path) {
        // Loop detected - show warning
        t := tree.Root(filepath.Base(path))
        warningStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("yellow"))
        t.Child(warningStyle.Render("⚠ Symlink loop detected"))
        return t
    }

    entries, err := os.ReadDir(path)
    if err != nil {
        return tree.Root(filepath.Base(path))
    }

    for _, entry := range entries {
        fullPath := filepath.Join(path, entry.Name())
        isSymlink := entry.Type()&os.ModeSymlink != 0

        if isSymlink {
            // Check if symlink points to directory
            targetInfo, err := os.Stat(fullPath)
            if err != nil {
                // Broken symlink
                brokenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("red"))
                t.Child(brokenStyle.Render(entry.Name() + " → (broken)"))
                continue
            }

            if targetInfo.IsDir() {
                // Symlinked directory - show with indicator
                targetPath, _ := os.Readlink(fullPath)
                displayName := entry.Name() + " → " + targetPath + "/"

                // Allow expansion like normal directories
                shouldExpand := nestingEnabled || expandedDirs[relPath]
                if shouldExpand {
                    // Recursively build (with loop protection)
                    subTree := buildTreeRecursiveWithMap(
                        fullPath, relPath, diffCache, gitignore,
                        respectIgnore, nestingEnabled, expandedDirs,
                        showHidden, lineNum, fileMap, dirMap, visited,
                    )
                    t.Child(subTree)
                } else {
                    symlinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("cyan"))
                    t.Child(symlinkStyle.Render(displayName))
                }
            } else {
                // Symlinked file
                targetPath, _ := os.Readlink(fullPath)
                displayName := entry.Name() + " → " + targetPath
                symlinkStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("cyan"))
                t.Child(symlinkStyle.Render(displayName))
            }
        } else {
            // Normal file/directory handling (existing code)
            ...
        }
    }
}
```

---

## Safety Measures

### 1. **Maximum Depth Limit**

```go
const MAX_SYMLINK_DEPTH = 10 // Prevent extremely deep chains

func buildTreeRecursiveWithMap(..., depth int) {
    if depth > MAX_SYMLINK_DEPTH {
        // Stop following symlinks
        return tree.Root("...")
    }
}
```

### 2. **Broken Symlink Handling**

```go
if err != nil {
    // Show broken symlink in red
    brokenStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("red"))
    name := entry.Name() + " → (broken)"
    t.Child(brokenStyle.Render(name))
    continue
}
```

### 3. **Security: Prevent Escaping Repository**

```go
func isWithinRepo(repoRoot, targetPath string) bool {
    absRepo, _ := filepath.Abs(repoRoot)
    absTarget, _ := filepath.Abs(targetPath)

    rel, err := filepath.Rel(absRepo, absTarget)
    if err != nil {
        return false
    }

    // Check if path tries to escape (starts with ..)
    return !strings.HasPrefix(rel, "..")
}
```

---

## User Experience Design

### Visual Language

**Symlink Indicators**:
- `→` : Regular symlink
- `⇒` : Alternative (could use for expanded symlinked dirs)
- Color: Cyan/blue to distinguish from regular entries

**Examples**:
```
📁 project/
├── 📁 src/
├── 📄 main.go
├── 📁 node_modules → ../shared/node_modules/  (cyan, collapsed)
└── 📄 config.json → /etc/app/config.json      (cyan)
```

**Expanded symlinked directory**:
```
📁 project/
├── 📁 node_modules → ../shared/node_modules/  (cyan, expanded)
│   ├── 📁 react/
│   └── 📁 express/
```

### Help Text Updates

Add to help screen:
```
Symlinks
────────
  Symlinks displayed with → indicator
  Cyan color indicates symlink
  Navigate symlinked dirs like normal dirs
  Loop detection prevents infinite recursion
  Broken symlinks shown in red
```

---

## Testing Strategy

### Test Cases

1. **Basic symlink to directory**
   - Create: `ln -s /path/to/dir linked_dir`
   - Expected: Shows as expandable directory with → indicator

2. **Symlink loop**
   - Create: `ln -s . loop` (self-referencing)
   - Expected: Detects loop, shows warning, doesn't crash

3. **Broken symlink**
   - Create: `ln -s /nonexistent broken`
   - Expected: Shows in red with "(broken)" label

4. **Symlink chain**
   - Create: A → B → C (three-level chain)
   - Expected: Follows chain, respects MAX_DEPTH

5. **Symlink outside repo**
   - Create: `ln -s /usr/local/bin tools`
   - Expected: (Optional) Warning or restriction

6. **Git diff with symlinked files**
   - Modify symlinked file
   - Expected: Shows git diff markers correctly

---

## Implementation Phases

### Phase 1: Detection & Display (v0.6.0)
- ✅ Detect symlinks using `entry.Type() & os.ModeSymlink`
- ✅ Distinguish symlinked dirs from symlinked files
- ✅ Add visual indicators (→, cyan color)
- ✅ Show broken symlinks in red
- ✅ Update help text

### Phase 2: Navigation (v0.6.0)
- ✅ Allow expanding symlinked directories
- ✅ Implement loop detection with visited paths
- ✅ Add maximum depth limit
- ✅ Handle broken symlinks gracefully

### Phase 3: Polish (v0.7.0)
- ⬜ Add configuration option to toggle symlink following
- ⬜ Show symlink target path in status bar on hover
- ⬜ Add keybinding to show full symlink chain
- ⬜ Performance optimization for large symlink trees

---

## Performance Considerations

### Benchmarking

Add benchmarks for:
- Directory scanning with symlinks
- Loop detection overhead
- Memory usage with visited paths tracking

### Optimization Ideas

1. **Lazy loop detection**: Only check for loops on expand
2. **Cache symlink resolution**: Store resolved paths
3. **Skip symlinks in benchmark mode**: For pure performance testing

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Infinite loops | App hangs | Visited paths tracking + max depth |
| Performance degradation | Slow tree builds | Benchmark before/after, optimize if needed |
| Security: escape repo | Access unintended files | Path validation (optional) |
| Broken symlinks | Confusing display | Clear visual indicator (red) |
| Cross-filesystem | Unexpected behavior | Document behavior, test edge cases |

---

## Configuration (Future)

Add to settings:
```go
type Config struct {
    FollowSymlinks     bool  // Default: true
    MaxSymlinkDepth    int   // Default: 10
    ShowSymlinkTargets bool  // Show target path
    WarnExternalLinks  bool  // Warn when leaving repo
}
```

---

## Questions to Resolve

1. **Should we restrict symlinks outside the repository?**
   - Pros: Security, prevents confusion
   - Cons: Reduces flexibility, common use case (e.g., `/usr/local`)

2. **How to handle relative vs absolute symlink paths in display?**
   - Show original path? Resolved path? Both?

3. **Should gitignore apply to symlink targets?**
   - Current behavior: gitignore checks apply to symlink paths
   - Should we also check target paths?

4. **Performance trade-off: Follow all symlinks or make it opt-in?**
   - Current plan: Follow by default (matches Unix tools)
   - Alternative: Add 's' key to toggle symlink following

---

## Success Criteria

✅ Users can navigate into symlinked directories
✅ Symlinks are visually distinct from regular entries
✅ No crashes from symlink loops
✅ Broken symlinks are clearly indicated
✅ Performance impact < 10% on directories with symlinks
✅ Comprehensive test coverage
✅ Updated documentation

---

## References

- [Go os.ReadDir Documentation](https://pkg.go.dev/os#ReadDir)
- [Go filepath.EvalSymlinks](https://pkg.go.dev/path/filepath#EvalSymlinks)
- [DirEntry Type Documentation](https://pkg.go.dev/io/fs#DirEntry)
- Research: "golang DirEntry IsDir symlink 2025" (see search results above)

---

## Next Steps

1. Review this plan with user
2. Create feature branch: `feature/symlink-support`
3. Implement Phase 1 (detection & display)
4. Implement Phase 2 (navigation & safety)
5. Add comprehensive tests
6. Update MEASUREMENTS.txt with performance impact
7. Release as v0.6.0
