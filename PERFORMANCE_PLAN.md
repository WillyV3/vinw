# vinw Performance Improvement Plan
**Date**: October 12, 2025
**Philosophy**: Unix principles - simple, modular, composable

---

## Current Performance Problem

**Observation**: vinw is slow in large directories.

**Root Cause Analysis**:

### Problem 1: Scanning Collapsed Directories (Critical)
**Location**: `main.go:1042-1086` in `buildTreeRecursiveWithMap()`

```go
entries, err := os.ReadDir(path)  // Line 1042 - READS EVERY DIRECTORY

for _, entry := range entries {
    if entry.IsDir() {
        shouldExpand := nestingEnabled || expandedDirs[relPath]
        if shouldExpand {
            // Recurse into directory
        } else {
            // Show "dirname/" but ALREADY WASTED I/O READING IT
        }
    }
}
```

**The bug**: We call `os.ReadDir()` at the START of the function (line 1042), before checking if directory should be expanded. This means:
- Large repo with 100 collapsed directories
- Each has 1,000 files inside
- We read 100,000 directory entries just to show "dirname/" for each

**Impact**: For collapsed view (default), we're reading the ENTIRE filesystem anyway, defeating the purpose of collapsing.

### Problem 2: Full Rebuild on Every Operation
- Line 687: Full tree rebuild every 5 seconds for git refresh
- Line 354, 399, 464: Full rebuild on every toggle
- Line 99-102 (`internal/github.go`): Counts lines in every untracked file

**Measurement needed**: Profile real repos to confirm bottlenecks before fixing.

---

## Unix Philosophy Applied

### Rule 1: Make each program do one thing well
**Current violation**: vinw does filesystem scanning, git diffing, tree building, and rendering in one monolithic loop.

**Fix**: Separate concerns into independent components that can be reasoned about and tested separately.

### Rule 2: Expect the output of every program to become the input to another
**Current violation**: Git diff data is tightly coupled to tree building.

**Fix**: Git diff should be a separate query that returns structured data. Tree building consumes it but doesn't depend on implementation.

### Rule 3: Design and build software to be tried early
**Current violation**: Can't test tree building without git, can't test git without filesystem.

**Fix**: Pure functions that can be tested with simple inputs. No side effects mixed with logic.

### Rule 4: Use tools in preference to unskilled help
**Current violation**: Custom line counting for untracked files (`github.go:99`).

**Fix**: Use `wc -l` or git's built-in capabilities. Don't reinvent.

---

## Research-Backed Changes

### Change 0: Only Scan Expanded Directories (CRITICAL)

**Current bug**: We read every directory even when collapsed.

**Fix**: Move `os.ReadDir()` inside the expand check.

**Before** (`main.go:1038-1092`):
```go
func buildTreeRecursiveWithMap(...) *tree.Tree {
    entries, err := os.ReadDir(path)  // WRONG: reads before checking if expanded
    if err != nil {
        return t
    }

    for _, entry := range entries {
        if entry.IsDir() {
            shouldExpand := nestingEnabled || expandedDirs[relPath]
            if shouldExpand {
                // recurse
            } else {
                // show collapsed
            }
        }
    }
}
```

**After**:
```go
func buildTreeRecursiveWithMap(...) *tree.Tree {
    dirName := filepath.Base(path)
    t := tree.Root(dirName)

    // If this directory should be collapsed, don't read it
    if relativePath != "" { // Not root directory
        shouldExpand := nestingEnabled || (expandedDirs != nil && expandedDirs[relativePath])
        if !shouldExpand {
            // Collapsed - don't read contents at all
            return t
        }
    }

    // Only read if expanded or root
    entries, err := os.ReadDir(path)
    if err != nil {
        return t
    }

    // Process entries...
}
```

**Wait, that won't work!** We're calling this recursively for CHILD directories, not the directory being shown.

**Correct approach**: Don't recurse into collapsed directories in the parent:

```go
for _, entry := range entries {
    if entry.IsDir() {
        // Track directory in dirMap
        dirMap[*lineNum] = relPath
        *lineNum++

        shouldExpand := nestingEnabled || (expandedDirs != nil && expandedDirs[relPath])

        if shouldExpand {
            // ONLY call ReadDir inside buildTreeRecursiveWithMap when we recurse
            subTree := buildTreeRecursiveWithMap(fullPath, relPath, ...)
            t.Child(subTree)
        } else {
            // Just show directory name, don't read contents
            dirStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("147"))
            displayName := entryName + "/"
            t.Child(dirStyle.Render(displayName))
        }
    }
}
```

**The key insight**: We need to know if a directory EXISTS before deciding to expand it, but we DON'T need to read its CONTENTS.

**Solution**: Use `os.Stat()` or `DirEntry.IsDir()` (already available from parent's ReadDir) to identify directories, but only call ReadDir when expanding.

**Benefit**: Collapsed directories = zero I/O inside them. Huge win for large repos.

**Trade-off**: Expanding a directory for the first time will trigger I/O (acceptable ~50-200ms delay).

---

### Change 1: Use filepath.WalkDir Instead of os.ReadDir

**Source**: Go 1.16+ stdlib, golang/go#41974 (2021-2025)

**Evidence**:
> "Walk is less efficient than WalkDir, introduced in Go 1.16, which avoids calling os.Lstat on every visited file or directory."

**Current code** (`main.go:1042`):
```go
entries, err := os.ReadDir(path)
```

**Problem**: Recursive calls to `os.ReadDir()` + `os.Stat()` checks = 2N syscalls for N files.

**Proposed**:
```go
filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
    // Single pass, DirEntry already has type info
    // No additional Stat calls needed
})
```

**Benefit**: 20-40% fewer syscalls. This is stdlib, zero dependencies, proven since 2021.

**Risk**: Low - stdlib function, well-tested. Only risk is refactoring tree builder.

---

### Change 2: Separate Git Diff from Tree Building

**Current design** (`main.go:687-688`):
```go
m.diffCache = internal.GetAllGitDiffs()
m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(...)
```

Tree rebuild happens every time git diff updates, even if diffs haven't changed.

**Unix approach**: Git diff is a separate query. Tree building is a separate transformation.

**Proposed architecture**:

```
┌─────────────┐
│   Git Diff  │ ← Separate module, returns map[path]lines
└─────────────┘
       ↓
┌─────────────┐
│  Tree Data  │ ← Pure data structure from filesystem
└─────────────┘
       ↓
┌─────────────┐
│  Renderer   │ ← Combines tree + diffs + selection → display
└─────────────┘
```

**Implementation**:

**Step 1**: Tree data becomes immutable after initial scan
```go
type TreeData struct {
    files []FileNode
    dirs  []DirNode
}

// Build once
treeData := scanFilesystem(rootPath)
```

**Step 2**: Rendering combines tree + current state
```go
type RenderState struct {
    tree      *TreeData      // Immutable filesystem data
    diffCache map[string]int // Mutable git state
    selection int            // Mutable UI state
    filters   FilterState    // Mutable filter state
}

func render(state RenderState) string {
    // Pure function: same inputs → same output
}
```

**Benefit**:
- Filesystem scan only happens on explicit refresh
- Git diff updates just update the map, no rescan
- Toggle operations only change filters, no rescan
- Each component testable independently

**Trade-off**: Initial scan needed. But that's acceptable - files don't change while you're looking at them.

---

### Change 3: Remove Line Counting from Untracked Files

**Current code** (`internal/github.go:89-104`):
```go
// Get untracked files and count their lines
cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
output, err = cmd.Output()
if err == nil {
    files := strings.Split(strings.TrimSpace(string(output)), "\n")
    for _, file := range files {
        if file == "" {
            continue
        }
        // Count all lines in untracked files
        lineCount := countFileLines(file)
        if lineCount > 0 {
            diffs[file] = lineCount
        }
    }
}
```

**Problem**: For repo with 100 untracked files, this opens and reads 100 files every 5 seconds.

**Question**: Do users need line counts for untracked files?

**Options**:

**A. Show marker without count**
```
newfile.go  (+)     ← Just show it's new, don't count
```

**B. Count only on demand**
```
Press 'i' on file → show full stats including line count
```

**C. Use git's numstat for staged files only**
```
Only show counts for files you've explicitly added with 'git add'
Untracked files just show '(new)' marker
```

**Recommendation**: Option A - show `(+)` marker without counting. If user wants details, they can `git diff` or open the file.

**Benefit**: Eliminates 100-300ms I/O every 5 seconds in repos with many untracked files.

---

### Change 4: Make Git Refresh Explicit

**Current**: Auto-refresh every 5 seconds (`main.go:969`)

**Unix philosophy**: Programs should be quiet unless they have something to say.

**Proposed**:
- Default: No auto-refresh (or very long interval like 60s)
- Press `r` to manually refresh git status
- Watch mode: `vinw --watch` enables auto-refresh for those who want it

**Benefit**:
- Predictable performance - no background surprises
- User controls when to pay the cost
- Simpler mental model

**Implementation**:
```go
case "r":
    // Manual refresh
    m.diffCache = internal.GetAllGitDiffs()
    return m, nil
```

---

### Change 5: Cache Filesystem State Until Explicit Refresh

**Current problem**: Every toggle rescans filesystem.

**Unix approach**: Separate read from transform.

**Design**:

```go
// On startup or 'R' key (capital R = full refresh)
fsState := readFilesystem(rootPath)

// On 'i' key (toggle gitignore)
visibleFiles = applyGitignore(fsState, respectIgnore)

// On 'n' key (toggle nesting)
treeView = applyNesting(visibleFiles, nestingEnabled)

// On 'h' key (toggle hidden)
treeView = applyHiddenFilter(visibleFiles, showHidden)
```

**Key insight**: Filesystem doesn't change while you're deciding which view you want.

**Commands**:
- `r` - Refresh git diff only (fast, ~50ms)
- `R` (capital) - Full refresh of filesystem + git (slow, ~500ms)
- Status bar shows last refresh time

**Benefit**: Instant toggles. User explicitly refreshes when they know files changed.

**Trade-off**: Requires user to press R after creating files externally. But that's fine - you know when you changed files.

---

## Proposed Architecture

### Simple Module Separation

```
internal/
├── fsscan.go      - Filesystem scanning (WalkDir)
├── gitdiff.go     - Git diff queries (renamed from github.go)
├── treedata.go    - Tree data structure (pure data)
├── filters.go     - Apply gitignore/hidden/nesting (pure functions)
└── render.go      - Combine data + state → display string

main.go            - Bubble Tea UI, coordinates modules
```

Each module:
- Single responsibility
- Testable independently
- No hidden dependencies
- Clear inputs/outputs

### Data Flow

```
User starts vinw
    ↓
Scan filesystem → TreeData (immutable)
    ↓
Query git diff → DiffMap (mutable)
    ↓
Apply filters → FilteredTree
    ↓
Render with selection → Display
    ↓
User presses key:
    - j/k: Update selection → re-render
    - i/n/h: Update filters → re-render
    - r: Query git → re-render
    - R: Rescan everything → start over
```

**No rebuild unless user asks for it.**

---

## Implementation Plan

### Phase 1: Measurement (Week 1)

Before changing anything, measure the actual bottlenecks.

```go
// Add timing to current code
start := time.Now()
m.tree, m.fileMap, m.dirMap = buildTreeWithMaps(...)
fmt.Fprintf(os.Stderr, "Tree rebuild: %v\n", time.Since(start))

start = time.Now()
m.diffCache = internal.GetAllGitDiffs()
fmt.Fprintf(os.Stderr, "Git diff: %v\n", time.Since(start))
```

Test with:
- Small repo (100 files)
- Medium repo (5,000 files)
- Large repo (20,000 files)
- Repo with many untracked files

**Goal**: Confirm hypotheses. Know which changes will have most impact.

**Deliverable**: Simple text file with timing data:
```
Repo: linux kernel (70,000 files)
Tree rebuild: 1,847ms
Git diff: 312ms
Git line counting: 1,203ms (89 untracked files)
```

### Phase 2: Extract Pure Functions (Week 2)

Separate data from logic without changing behavior.

**2.1**: Create `internal/fsscan.go`
```go
type FileNode struct {
    Path    string
    Name    string
    IsDir   bool
    ModTime time.Time
}

func ScanFilesystem(rootPath string) ([]FileNode, error) {
    // Use filepath.WalkDir
    // Return pure data, no rendering
}
```

**2.2**: Create `internal/filters.go`
```go
func ApplyGitignore(nodes []FileNode, gi *GitIgnore, respect bool) []FileNode {
    // Pure function: same inputs → same outputs
}

func ApplyHiddenFilter(nodes []FileNode, showHidden bool) []FileNode {
    // Pure function
}

func ApplyNesting(nodes []FileNode, enabled bool, expanded map[string]bool) []FileNode {
    // Pure function
}
```

**2.3**: Update `main.go` to use new functions
```go
// Initial scan
m.fsNodes = fsscan.ScanFilesystem(rootPath)

// On toggle
filtered := filters.ApplyGitignore(m.fsNodes, m.gitignore, m.respectIgnore)
filtered = filters.ApplyHiddenFilter(filtered, m.showHidden)
m.visibleNodes = filters.ApplyNesting(filtered, m.nestingEnabled, m.expandedDirs)
```

**Benefit**: Code becomes testable and understandable. Performance not worse (baseline).

### Phase 3: Optimize the Separated Modules (Week 3)

Now that modules are separate, optimize each independently.

**3.1**: Replace `os.ReadDir` with `filepath.WalkDir` in `fsscan.go`
```go
func ScanFilesystem(rootPath string) ([]FileNode, error) {
    var nodes []FileNode
    err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
        if err != nil {
            return err
        }
        // Skip .git
        if d.Name() == ".git" {
            return filepath.SkipDir
        }
        nodes = append(nodes, FileNode{
            Path:  path,
            Name:  d.Name(),
            IsDir: d.IsDir(),
        })
        return nil
    })
    return nodes, err
}
```

**3.2**: Remove line counting from `gitdiff.go` (was `github.go`)
```go
// Untracked files: just mark as new, don't count
cmd = exec.Command("git", "ls-files", "--others", "--exclude-standard")
output, err = cmd.Output()
if err == nil {
    files := strings.Split(strings.TrimSpace(string(output)), "\n")
    for _, file := range files {
        if file != "" {
            diffs[file] = -1  // -1 = new file, display as "(+)"
        }
    }
}
```

**3.3**: Add manual refresh keybindings
```go
case "r":
    // Refresh git only
    m.diffCache = internal.GetAllGitDiffs()
    return m, nil
case "R":
    // Full refresh
    m.fsNodes = fsscan.ScanFilesystem(m.rootPath)
    m.diffCache = internal.GetAllGitDiffs()
    // Reapply filters
    return m, nil
```

**3.4**: Remove automatic tick or make it 60s
```go
func tick() tea.Cmd {
    // Option 1: No auto-refresh
    return nil

    // Option 2: Very infrequent
    return tea.Tick(60*time.Second, ...)
}
```

**Benefit**: Measured improvement from Phase 1 benchmarks.

### Phase 4: Measure Again (Week 4)

Run same benchmarks as Phase 1.

**Expected results**:
- Tree rebuild: 20-40% faster (WalkDir)
- Git diff: 3-10x faster (no line counting)
- Toggle operations: Near instant (no rescan)

**If results aren't satisfactory**: Phase 1 measurements tell us where to look next.

---

## Testing Strategy

### Unit Tests (New)

```go
// internal/fsscan_test.go
func TestScanFilesystem(t *testing.T) {
    // Create temp directory structure
    // Scan it
    // Assert correct files found
}

// internal/filters_test.go
func TestApplyGitignore(t *testing.T) {
    nodes := []FileNode{
        {Path: "file.go"},
        {Path: "node_modules/foo.js"},
    }
    gi := &GitIgnore{patterns: []string{"node_modules/"}}

    result := ApplyGitignore(nodes, gi, true)

    // Assert node_modules filtered out
}
```

### Integration Tests

```go
// main_test.go
func TestToggleGitignore(t *testing.T) {
    // Start app with test repo
    // Send 'i' key message
    // Assert tree updated without filesystem rescan
}
```

### Manual Testing Checklist

For each change, verify:
- [ ] Small repo (100 files): No regression, feels instant
- [ ] Medium repo (5,000 files): Smooth navigation, fast toggles
- [ ] Large repo (20,000 files): Acceptable startup, instant toggles
- [ ] All keybindings work: j/k/i/n/h/r/R/a/A/d/c
- [ ] Git markers appear correctly
- [ ] Create/delete files updates correctly after 'R'

---

## Risk Mitigation

### Risk: Breaking git integration
**Mitigation**: Keep existing tests passing. Git diff output format is stable.

### Risk: WalkDir ordering different from ReadDir
**Mitigation**: Both return sorted entries. If needed, explicit sort.

### Risk: Users forget to refresh after external changes
**Mitigation**:
- Status bar shows "Last refresh: 5s ago"
- Gentle reminder after 60s: "Press R to refresh"
- Optional `--watch` flag for auto-refresh

### Risk: Phase 2 refactor introduces bugs
**Mitigation**:
- Refactor without behavior change first
- Add tests that pass before and after
- Manual testing with checklist

---

## Non-Goals (What We're NOT Doing)

### ❌ Concurrent/Parallel Scanning
**Why not**: Premature optimization. Single-threaded WalkDir is fast enough. Adds complexity for minimal gain on modern SSDs.

### ❌ File System Watching (fsnotify)
**Why not**: Adds complexity, requires event handling, can miss events. Explicit refresh is more predictable.

### ❌ Virtual Scrolling / Windowing
**Why not**: Bubble Tea viewport already handles this efficiently. No evidence it's a bottleneck.

### ❌ Lazy Loading Directories
**Why not**: Adds state complexity. Phase 3 approach (cache + filter) is simpler and fast enough.

### ❌ Custom Data Structures (trees, tries, etc.)
**Why not**: Go slices and maps are fast. Premature optimization. Keep it simple.

---

## Success Criteria

### Qualitative
- Code is simpler and more maintainable than current version
- Each module can be understood independently
- New contributors can add features easily

### Quantitative
- Medium repos (5,000 files): All operations feel instant (<50ms)
- Large repos (20,000 files): Toggles <100ms, startup acceptable
- No operation triggers unnecessary filesystem rescans

### Philosophy
- Unix principles maintained throughout
- No unnecessary complexity
- Each tool does one thing well

---

## Decision: Go or No-Go?

**Review this plan and decide**:

1. Does Phase 1 measurement make sense? (baseline before changes)
2. Is Phase 2 extraction worthwhile? (testability, maintainability)
3. Are Phase 3 optimizations sound? (WalkDir, remove line counting, manual refresh)

**If yes**: Start with Phase 1 measurement this week.

**If concerns**: Let's discuss specific parts before committing.

**Key question**: Is manual refresh (press R) acceptable? Or must it be automatic?

---

## Summary

**Problem**: vinw is slow in large directories due to full rescans.

**Root cause**: Filesystem read + tree build + rendering are coupled. Every state change triggers full rescan.

**Solution**: Separate concerns. Read once, transform many times, render on demand.

**Approach**:
1. Measure current performance (know the enemy)
2. Extract pure functions (make testable)
3. Optimize independently (WalkDir, remove line counting, manual refresh)
4. Measure again (verify improvement)

**Philosophy**: Unix principles throughout. Simple modules. Clear boundaries. No premature optimization.

**Expected outcome**:
- Code is simpler
- Tests are possible
- Performance is better
- Future features are easier

**Next step**: Review and approve/adjust plan.
