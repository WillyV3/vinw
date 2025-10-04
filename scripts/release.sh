#!/usr/bin/env bash
#
# Fast Local Release Script for Go Projects
# Automates the complete release process using recent changes as context
#
# Usage:
#   ./release.sh patch                     # Auto-detect patch release
#   ./release.sh minor                     # Auto-detect minor release
#   ./release.sh major                     # Auto-detect major release
#   ./release.sh v1.2.3 "Custom message"  # Specific version
#

set -euo pipefail

# Configuration
readonly SCRIPT_NAME="release.sh"
readonly PROJECT_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"

# Colors for output
readonly RED='\033[0;31m'
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly BLUE='\033[0;34m'
readonly CYAN='\033[0;36m'
readonly BOLD='\033[1m'
readonly NC='\033[0m'

# Output functions
print_header() {
    echo -e "\n${CYAN}${BOLD}=== $1 ===${NC}"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_info() {
    echo -e "${BLUE}→${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1" >&2
}

# Check if gum is available
HAS_GUM=false
if command -v gum &> /dev/null; then
    HAS_GUM=true
fi

# Check prerequisites
check_prerequisites() {
    local missing_tools=()

    command -v git >/dev/null 2>&1 || missing_tools+=("git")
    command -v gh >/dev/null 2>&1 || missing_tools+=("gh")
    command -v go >/dev/null 2>&1 || missing_tools+=("go")

    if [[ ${#missing_tools[@]} -gt 0 ]]; then
        print_error "Missing required tools: ${missing_tools[*]}"
        exit 1
    fi

    # Check if we're in a git repository
    if ! git rev-parse --git-dir >/dev/null 2>&1; then
        print_error "Not in a git repository"
        exit 1
    fi

    # Check if we have a GitHub remote
    if ! git remote get-url origin >/dev/null 2>&1; then
        print_error "No origin remote found"
        exit 1
    fi
}

# Run tests and linting quickly
run_checks() {
    print_header "Running Quick Checks"

    # Run tests
    print_info "Running tests..."
    if go test ./...; then
        print_success "Tests passed"
    else
        print_error "Tests failed"
        exit 1
    fi

    # Run linting if available
    if command -v golangci-lint >/dev/null 2>&1; then
        print_info "Running linter..."
        if golangci-lint run; then
            print_success "Linting passed"
        else
            print_error "Linting failed"
            exit 1
        fi
    else
        print_warning "golangci-lint not found, skipping linting"
    fi

    # Build check
    print_info "Running build check..."

    # Create temp directory for builds
    local temp_dir="/tmp/build-test-$$"
    mkdir -p "$temp_dir"

    # Try different build strategies based on project structure
    local build_success=false

    # Strategy 1: Try building the main package in current directory
    if [[ -f "main.go" ]] || ls *.go &>/dev/null; then
        if go build -o "$temp_dir/main" .; then
            build_success=true
        fi
    fi

    # Strategy 2: Try building cmd/ directory if it exists
    if [[ "$build_success" == "false" && -d "cmd" ]]; then
        for cmd_dir in cmd/*/; do
            if [[ -d "$cmd_dir" ]]; then
                local cmd_name=$(basename "$cmd_dir")
                if go build -o "$temp_dir/$cmd_name" "$cmd_dir"; then
                    build_success=true
                    break
                fi
            fi
        done
    fi

    # Strategy 3: Try building all packages separately
    if [[ "$build_success" == "false" ]]; then
        if go build -o "$temp_dir/" ./...; then
            build_success=true
        fi
    fi

    # Clean up
    rm -rf "$temp_dir"

    if [[ "$build_success" == "true" ]]; then
        print_success "Build successful"
    else
        print_error "Build failed - could not determine build strategy for this Go project"
        exit 1
    fi
}

# Analyze recent git changes
analyze_recent_changes() {
    print_header "Analyzing Recent Changes"

    # Get recent commits
    print_info "Recent commits:"
    git log --oneline -5 --color=always | sed 's/^/  /'

    # Get current status
    if [[ -n $(git status --porcelain) ]]; then
        print_warning "Working directory has uncommitted changes"
        git status --short | sed 's/^/  /'
        echo
    fi

    # Get recent changes stats
    if git rev-parse HEAD~1 >/dev/null 2>&1; then
        print_info "Recent changes summary:"
        git diff HEAD~1..HEAD --stat | sed 's/^/  /'
    fi
}

# Determine current version
get_current_version() {
    # Get all version tags, filter for valid semver, and get the latest
    local latest_tag
    latest_tag=$(git tag -l | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -1)

    if [[ -z "$latest_tag" ]]; then
        echo "0.0.0"  # No valid version tags found, start from 0.0.0
    else
        echo "${latest_tag#v}"  # Remove 'v' prefix if present
    fi
}

# Increment version based on type with conflict detection
increment_version() {
    local current_version="$1"
    local release_type="$2"

    # Handle first release case
    if [[ "$current_version" == "0.0.0" ]]; then
        case "${release_type}" in
            major)
                echo "1.0.0"
                ;;
            minor)
                echo "0.1.0"
                ;;
            patch|*)
                echo "0.0.1"
                ;;
        esac
        return
    fi

    # Parse version components
    local major minor patch
    IFS='.' read -r major minor patch <<< "${current_version}"
    major=${major:-0}
    minor=${minor:-0}
    patch=${patch:-0}

    # Calculate new version with conflict detection
    local next_version
    while true; do
        case "${release_type}" in
            major)
                major=$((major + 1))
                minor=0
                patch=0
                ;;
            minor)
                minor=$((minor + 1))
                patch=0
                ;;
            patch|*)
                patch=$((patch + 1))
                ;;
        esac

        next_version="${major}.${minor}.${patch}"

        # Check if this version already exists
        if ! git tag -l | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | grep -q "^v${next_version}$"; then
            break
        fi

        # If it exists, increment patch and try again
        print_warning "Tag v$next_version already exists, trying next version..." >&2
        release_type="patch"
    done

    echo "$next_version"
}

# Generate release notes from recent commits
generate_release_notes() {
    local release_type="$1"
    local new_version="$2"

    print_info "Generating release notes from recent commits..." >&2

    # Get commits since last tag
    local last_tag
    last_tag=$(git describe --tags --abbrev=0 2>/dev/null || echo "")

    local commit_range
    if [[ -n "${last_tag}" ]]; then
        commit_range="${last_tag}..HEAD"
    else
        commit_range="HEAD~5..HEAD"
    fi

    # Analyze commit messages for features, fixes, etc.
    local commits
    commits=$(git log --pretty=format:"- %s" "${commit_range}" 2>/dev/null || echo "- Initial release")

    # Categorize commits
    local features=""
    local improvements=""
    local fixes=""
    local other=""

    while IFS= read -r commit; do
        local lower_commit=$(echo "$commit" | tr '[:upper:]' '[:lower:]')
        if [[ $lower_commit =~ (feat|feature|add|new) ]]; then
            features="${features}${commit}\n"
        elif [[ $lower_commit =~ (fix|bug|patch|resolve) ]]; then
            fixes="${fixes}${commit}\n"
        elif [[ $lower_commit =~ (improve|enhance|update|optimize|refactor) ]]; then
            improvements="${improvements}${commit}\n"
        else
            other="${other}${commit}\n"
        fi
    done <<< "$commits"

    # Generate release title
    local release_title
    case "${release_type}" in
        major)
            release_title="Major Release v${new_version}"
            ;;
        minor)
            release_title="New Features & Improvements v${new_version}"
            ;;
        patch)
            release_title="Bug Fixes & Improvements v${new_version}"
            ;;
        *)
            release_title="Release v${new_version}"
            ;;
    esac

    # Build release notes
    local notes="# ${release_title}\n\n"

    if [[ -n "${features}" ]]; then
        notes="${notes}## 🚀 New Features\n${features}\n"
    fi

    if [[ -n "${improvements}" ]]; then
        notes="${notes}## 💡 Improvements\n${improvements}\n"
    fi

    if [[ -n "${fixes}" ]]; then
        notes="${notes}## 🐛 Bug Fixes\n${fixes}\n"
    fi

    if [[ -n "${other}" ]]; then
        notes="${notes}## 📝 Other Changes\n${other}\n"
    fi

    # Add installation/usage info
    local repo_url
    repo_url=$(git remote get-url origin | sed 's/\.git$//')
    local github_user repo_name
    github_user=$(echo "$repo_url" | sed -n 's|.*github\.com[:/]\([^/]*\)/.*|\1|p')
    repo_name=$(basename "$repo_url")

    notes="${notes}---\n\n"
    if [[ -f "go.mod" ]]; then
        notes="${notes}**Installation**: \`go install ${repo_url}@v${new_version}\`\n\n"
        if [[ -d "$HOME/homebrew-tap" ]]; then
            notes="${notes}**Homebrew**: \`brew install ${github_user}/tap/${repo_name}\`\n\n"
        fi
    fi
    notes="${notes}**Full Changelog**: ${repo_url}/compare/$(get_current_version)...v${new_version}"

    echo -e "$notes"
}

# Build release binaries using GoReleaser
build_release_binaries() {
    local version="$1"

    print_header "Building Release Binaries"

    if command -v goreleaser >/dev/null 2>&1; then
        print_info "Using GoReleaser for cross-platform builds..."

        # Set version for GoReleaser
        export GORELEASER_CURRENT_TAG="v${version}"

        if goreleaser build --clean --snapshot; then
            print_success "GoReleaser build completed"
        else
            print_warning "GoReleaser build failed, falling back to simple build"
            # Fallback to simple build
            print_info "Building for current platform..."
            go build -ldflags "-s -w -X main.version=v${version}" -o "dist/$(basename "$PROJECT_ROOT")" .
            print_success "Single platform build completed"
        fi
    else
        print_info "GoReleaser not found, building for current platform..."
        mkdir -p dist
        go build -ldflags "-s -w -X main.version=v${version}" -o "dist/$(basename "$PROJECT_ROOT")" .
        print_success "Single platform build completed"
    fi
}

# Create and push git tag
create_git_tag() {
    local version="$1"
    local tag="v${version}"

    print_header "Creating Git Tag"

    # Check if tag already exists
    if git tag -l | grep -q "^${tag}$"; then
        print_error "Tag $tag already exists"
        exit 1
    fi

    # Create tag
    git tag "$tag"
    print_success "Created tag: $tag"

    # Push tag
    git push origin "$tag"
    print_success "Pushed tag to origin"
}

# Create GitHub release with assets
create_github_release() {
    local version="$1"
    local title="$2"
    local notes="$3"
    local tag="v${version}"

    print_header "Creating GitHub Release"

    # Create release using gh CLI
    local release_args=("$tag" --title "$title" --notes "$notes")

    # Add assets if they exist
    if [[ -d "dist" ]]; then
        print_info "Adding release assets..."
        local assets=(dist/*)
        if [[ ${#assets[@]} -gt 0 && -f "${assets[0]}" ]]; then
            release_args+=("${assets[@]}")
        fi
    fi

    local release_url
    release_url=$(gh release create "${release_args[@]}")

    print_success "Created GitHub release: $release_url"
    echo "$release_url"
}

# Update Homebrew tap (if applicable)
update_homebrew_tap() {
    local version="$1"

    print_header "Updating Homebrew Tap"

    # Detect project info
    local github_user repo_name go_module_name formula_name
    github_user=$(git remote get-url origin | sed -n 's|.*github\.com[:/]\([^/]*\)/.*|\1|p')
    repo_name=$(git remote get-url origin | sed 's|.*github\.com[:/][^/]*/||' | sed 's|\.git$||')

    # Get the Go module name from go.mod for formula name
    if [[ -f "go.mod" ]]; then
        go_module_name=$(grep -E '^module ' go.mod | cut -d' ' -f2)
        formula_name=$(basename "$go_module_name")
    else
        formula_name="$repo_name"
    fi

    if [[ -z "$github_user" ]]; then
        print_info "Could not detect GitHub username, skipping Homebrew update"
        return
    fi

    local tap_dir="$HOME/homebrew-tap"
    local formula_file="$tap_dir/Formula/${formula_name}.rb"

    if [[ ! -d "$tap_dir" ]]; then
        print_info "No Homebrew tap directory found at $tap_dir"
        return
    fi

    if [[ ! -f "$formula_file" ]]; then
        # Try with repo name as fallback
        formula_file="$tap_dir/Formula/${repo_name}.rb"
        if [[ ! -f "$formula_file" ]]; then
            # Try with nextui as another fallback (common shortened name)
            formula_file="$tap_dir/Formula/nextui.rb"
            if [[ ! -f "$formula_file" ]]; then
                print_info "No Homebrew formula found for $formula_name, $repo_name, or nextui"
                return
            fi
            formula_name="nextui"
        else
            formula_name="$repo_name"
        fi
    fi

    print_info "Found Homebrew formula: $formula_file"

    # Wait for GitHub to process the tag
    print_info "Waiting 10 seconds for GitHub to process the tag..."
    sleep 10

    # Download and verify the new tarball
    local tarball_url="https://github.com/${github_user}/${repo_name}/archive/v${version}.tar.gz"
    local temp_tarball="/tmp/${formula_name}-v${version}.tar.gz"

    print_info "Downloading tarball: $tarball_url"

    # Download with extended retry logic
    local download_success=false
    for attempt in 1 2 3 4 5; do
        print_info "Download attempt $attempt/5..."
        if curl -L "$tarball_url" -o "$temp_tarball" --fail --silent --show-error --max-time 30; then
            # Verify the download
            if [[ -f "$temp_tarball" && -s "$temp_tarball" ]]; then
                download_success=true
                break
            else
                print_warning "Downloaded file is empty or corrupt"
            fi
        fi

        if [[ $attempt -lt 5 ]]; then
            local wait_time=$((attempt * 3))
            print_warning "Download failed, waiting ${wait_time} seconds before retry..."
            sleep $wait_time
        fi
    done

    if [[ "$download_success" != "true" ]]; then
        print_error "Failed to download tarball after 5 attempts: $tarball_url"
        print_info "GitHub may still be processing the release. Try again in a few minutes."
        return 1
    fi

    # Calculate SHA256
    local new_sha256
    new_sha256=$(shasum -a 256 "$temp_tarball" | cut -d' ' -f1)
    print_info "Calculated SHA256: $new_sha256"

    # Backup original formula
    cp "$formula_file" "$formula_file.backup"

    # Update formula file with more robust regex
    print_info "Updating formula file..."
    sed -i.tmp \
        -e "s|archive/v[0-9]*\.[0-9]*\.[0-9]*\.tar\.gz|archive/v${version}.tar.gz|g" \
        -e "s|sha256 \"[a-f0-9]*\"|sha256 \"${new_sha256}\"|g" \
        "$formula_file"

    # Clean up temp files
    rm -f "$formula_file.tmp"

    # Verify changes were applied correctly
    if grep -q "v${version}.tar.gz" "$formula_file" && grep -q "$new_sha256" "$formula_file"; then
        print_success "Formula updated successfully"

        # Show the changes
        print_info "Changes made:"
        if command -v colordiff >/dev/null 2>&1; then
            colordiff "$formula_file.backup" "$formula_file" || true
        else
            diff "$formula_file.backup" "$formula_file" || true
        fi

        # Commit and push changes with better error handling
        print_info "Committing and pushing changes..."
        (
            cd "$tap_dir"

            # Ensure we're on the right branch
            local current_branch
            current_branch=$(git branch --show-current)

            git add "Formula/${formula_name}.rb"

            if git commit -m "Update ${formula_name} to v${version}

WillyV3 Generated with BreakingShit"; then

                # Try to push to current branch first, then try main/master
                if git push origin "$current_branch" 2>/dev/null || \
                   git push origin main 2>/dev/null || \
                   git push origin master 2>/dev/null; then
                    print_success "Changes pushed to GitHub"
                else
                    print_error "Failed to push changes to GitHub"
                    print_info "You may need to manually push the tap changes"
                    return 1
                fi
            else
                print_error "Failed to commit changes (may already be committed)"
                # Check if the change is already there
                if git diff --quiet && git diff --cached --quiet; then
                    print_info "No changes to commit - formula may already be up to date"
                else
                    return 1
                fi
            fi
        )

        # Clear Homebrew cache to force fresh download
        print_info "Clearing Homebrew cache for $formula_name..."
        brew cleanup --prune=all >/dev/null 2>&1 || true

        # Force Homebrew to update
        print_info "Updating Homebrew to pick up changes..."
        brew update >/dev/null 2>&1 || true

        print_success "Successfully updated Homebrew formula to v${version}"
        print_info "Users can now install with: brew install ${github_user}/tap/${formula_name}"

        # Clean up
        rm -f "$temp_tarball" "$formula_file.backup"

    else
        print_error "Formula update failed - changes not applied correctly"
        # Restore backup
        mv "$formula_file.backup" "$formula_file"
        rm -f "$temp_tarball"
        return 1
    fi
}

# Verify release completion
verify_release() {
    local version="$1"
    local tag="v${version}"

    print_header "Verifying Release"

    # Check git tag
    if git tag -l | grep -q "^${tag}$"; then
        print_success "Git tag created: $tag"
    else
        print_error "Git tag not found: $tag"
        return 1
    fi

    # Check GitHub release
    if gh release view "$tag" >/dev/null 2>&1; then
        print_success "GitHub release created: $tag"
    else
        print_error "GitHub release not found: $tag"
        return 1
    fi

    print_success "Release verification complete!"
}

# Interactive release configuration
configure_release() {
    local suggested_type="$1"
    local suggested_version="$2"
    local auto_notes="$3"
    local is_interactive_mode="$4"

    if [[ "$HAS_GUM" != "true" ]] || [[ "$is_interactive_mode" != "true" ]]; then
        # Fallback for no gum or non-interactive mode - use sensible defaults
        # Check if Homebrew tap exists for automatic update
        local should_update_homebrew="false"
        if [[ -d "$HOME/homebrew-tap" ]]; then
            should_update_homebrew="true"
        fi
        # Clean the auto_notes to remove any problematic characters and escape newlines
        local clean_notes
        clean_notes=$(echo "$auto_notes" | sed 's/\x1b\[[0-9;]*[mGKHF]//g' | tr '|' ',' | sed ':a;N;$!ba;s/\n/\\n/g')
        echo "$suggested_type|$suggested_version|$clean_notes|true|$should_update_homebrew|false"
        return
    fi

    print_header "Release Configuration" >&2

    # Show current status
    gum style --foreground 33 "📋 Current Release Plan:" >&2
    printf "Type,Value\nSuggested Type,%s\nSuggested Version,%s\nAuto-generated Notes,Yes\nBuild Binaries,Yes\nUpdate Homebrew,Yes\n" \
        "$suggested_type" "$suggested_version" | \
        gum table --separator "," --columns "Type,Value" >&2
    echo >&2

    # Ask if user wants to customize
    if gum confirm "Customize release settings?"; then
        echo >&2

        # Version selection
        gum style --foreground 33 "🎯 Version Configuration:" >&2
        local version_choice
        version_choice=$(gum choose --header "Choose version approach:" \
            "Auto-increment ($suggested_type → $suggested_version)" \
            "Custom version number" \
            "Skip release (dry run)")

        local final_version="$suggested_version"
        local final_type="$suggested_type"
        local is_dry_run="false"

        case "$version_choice" in
            "Custom version number")
                final_version=$(gum input --prompt "Enter version (without 'v' prefix): " --placeholder "1.2.3")
                final_type="custom"
                ;;
            "Skip release (dry run)")
                is_dry_run="true"
                ;;
        esac

        echo >&2

        # Release notes
        gum style --foreground 33 "📝 Release Notes:" >&2
        local notes_choice
        notes_choice=$(gum choose --header "Choose release notes approach:" \
            "Auto-generate from commits" \
            "Write custom notes" \
            "Edit auto-generated notes")

        local final_notes="$auto_notes"
        case "$notes_choice" in
            "Write custom notes")
                final_notes=$(gum write --header "Write your release notes:" --placeholder "# Release Title

## What's New
- Feature 1
- Feature 2

## Bug Fixes
- Fix 1
- Fix 2")
                ;;
            "Edit auto-generated notes")
                final_notes=$(echo "$auto_notes" | gum write --header "Edit the auto-generated release notes:")
                ;;
        esac

        echo >&2

        # Build options
        gum style --foreground 33 "🔨 Build Configuration:" >&2
        local build_binaries="true"
        if gum confirm "Build release binaries?"; then
            build_binaries="true"
        else
            build_binaries="false"
        fi

        # Homebrew update - automatic if tap exists, offer to create if missing
        # Don't use 'local' here - we need this variable in the main function
        update_homebrew="false"
        if [[ -d "$HOME/homebrew-tap" ]]; then
            # Automatically update existing tap - no need to ask!
            update_homebrew="true"
            print_success "✅ Will automatically update Homebrew tap" >&2
        else
            # Offer to create homebrew tap if missing
            if gum confirm --default=no "No Homebrew tap found. Create one at ~/homebrew-tap?"; then
                print_info "Creating Homebrew tap structure..." >&2
                mkdir -p "$HOME/homebrew-tap/Formula"
                cd "$HOME/homebrew-tap"
                git init
                echo "# $(whoami)'s Homebrew Tap" > README.md
                git add README.md
                git commit -m "Initial commit"
                print_success "Created Homebrew tap at ~/homebrew-tap" >&2
                update_homebrew="true"
            else
                print_info "Skipping Homebrew tap setup" >&2
            fi
        fi

        echo >&2

        # Show final configuration
        gum style --foreground 46 "✅ Final Configuration:" >&2
        local config_table
        if [[ "$is_dry_run" == "true" ]]; then
            config_table="Action,Value\nMode,Dry Run (no actual release)\nVersion,$final_version\nBuild Binaries,$build_binaries"
        else
            config_table="Action,Value\nRelease Type,$final_type\nVersion,$final_version\nBuild Binaries,$build_binaries\nUpdate Homebrew,$update_homebrew"
        fi
        echo "$config_table" | gum table --separator "," --columns "Action,Value" >&2

        echo >&2
        if ! gum confirm "Proceed with this configuration?"; then
            print_error "Release cancelled by user" >&2
            exit 1
        fi

        # Return configuration - escape newlines in notes
        local escaped_final_notes="${final_notes//$'\n'/\\n}"
        echo "$final_type|$final_version|$escaped_final_notes|$build_binaries|$update_homebrew|$is_dry_run"
    else
        # Use defaults - check if Homebrew tap exists
        local default_update_homebrew="false"
        if [[ -d "$HOME/homebrew-tap" ]]; then
            default_update_homebrew="true"
        fi
        # Escape newlines in notes to prevent parsing issues
        local escaped_notes="${auto_notes//$'\n'/\\n}"
        echo "$suggested_type|$suggested_version|$escaped_notes|true|$default_update_homebrew|false"
    fi
}

# Smart file categorization
categorize_files() {
    # Always commit: Go code and project files
    local auto_commit_patterns=("*.go" "*.mod" "*.sum" "go.work" "go.work.sum")

    # Ask user: Documentation and config files
    local user_choice_patterns=("*.md" "*.txt" "*.json" "*.yaml" "*.yml" "*.toml")

    # Never commit: Binaries and temp files
    local ignore_patterns=("*.out" "*.exe" "**/bin/*" "**/dist/*" "**/.DS_Store" "**/node_modules/*")

    # Initialize global arrays (remove 'local' to make them global)
    auto_commit_files=()
    user_choice_files=()
    ignore_files=()
    other_files=()

    # Get all changed files (modified + untracked)
    local all_files
    all_files=$(git diff --name-only; git ls-files --others --exclude-standard)

    while IFS= read -r file; do
        [[ -z "$file" ]] && continue

        local categorized=false

        # Check auto-commit patterns
        for pattern in "${auto_commit_patterns[@]}"; do
            if [[ "$file" == $pattern ]]; then
                auto_commit_files+=("$file")
                categorized=true
                break
            fi
        done

        [[ "$categorized" == true ]] && continue

        # Check user choice patterns
        for pattern in "${user_choice_patterns[@]}"; do
            if [[ "$file" == $pattern ]]; then
                user_choice_files+=("$file")
                categorized=true
                break
            fi
        done

        [[ "$categorized" == true ]] && continue

        # Check ignore patterns
        for pattern in "${ignore_patterns[@]}"; do
            if [[ "$file" == $pattern ]]; then
                ignore_files+=("$file")
                categorized=true
                break
            fi
        done

        [[ "$categorized" == true ]] && continue

        # Everything else goes to "other"
        other_files+=("$file")

    done <<< "$all_files"

    # Note: Arrays are automatically global in bash functions unless declared local
    # These arrays are now available to other functions
}

# Handle uncommitted changes with smart categorization
handle_uncommitted_changes() {
    print_header "Smart Git Management"

    # Check if there are any uncommitted changes
    if git diff --quiet && git diff --cached --quiet; then
        local untracked_files
        untracked_files=$(git ls-files --others --exclude-standard)

        if [[ -z "$untracked_files" ]]; then
            print_success "Working directory is clean"
            return 0
        fi
    fi

    # Categorize all files
    categorize_files

    # Show categorized files
    show_file_categories

    if [[ "$HAS_GUM" == "true" ]]; then
        # Smart interactive mode
        local action
        action=$(gum choose --header "How would you like to handle these changes?" \
            "Smart commit (recommended)" \
            "Custom file selection" \
            "Stash all changes" \
            "Continue anyway (risky)" \
            "Abort release")

        case "$action" in
            "Smart commit (recommended)")
                smart_commit_workflow
                ;;
            "Custom file selection")
                interactive_commit_workflow
                ;;
            "Stash all changes")
                print_info "Stashing all uncommitted changes..."
                git stash push -m "Auto-stash before release $(date +%Y%m%d-%H%M%S)" || {
                    print_error "Failed to stash changes"
                    exit 1
                }
                print_success "Changes stashed successfully"
                ;;
            "Continue anyway (risky)")
                print_warning "Continuing with uncommitted changes..."
                ;;
            "Abort release")
                print_info "Release aborted by user"
                exit 0
                ;;
        esac
    else
        # Fallback for no gum
        echo "Uncommitted changes detected. Please commit or stash them before releasing."
        exit 1
    fi
}

# Show categorized files
show_file_categories() {
    echo

    if [[ ${#auto_commit_files[@]} -gt 0 ]]; then
        print_success "✅ Will auto-commit (Go code & project files):"
        printf '%s\n' "${auto_commit_files[@]}" | sed 's/^/  /'
        echo
    fi

    if [[ ${#user_choice_files[@]} -gt 0 ]]; then
        print_info "❓ Will ask about (docs & config files):"
        printf '%s\n' "${user_choice_files[@]}" | sed 's/^/  /'
        echo
    fi

    if [[ ${#ignore_files[@]} -gt 0 ]]; then
        print_warning "🚫 Will ignore (binaries & temp files):"
        printf '%s\n' "${ignore_files[@]}" | sed 's/^/  /'
        echo
    fi

    if [[ ${#other_files[@]} -gt 0 ]]; then
        print_info "❔ Unknown file types (will ask):"
        printf '%s\n' "${other_files[@]}" | sed 's/^/  /'
        echo
    fi
}

# Smart commit workflow
smart_commit_workflow() {
    print_header "Smart Commit Workflow"

    local files_to_commit=()

    # Auto-add Go code and project files
    if [[ ${#auto_commit_files[@]} -gt 0 ]]; then
        for file in "${auto_commit_files[@]}"; do
            git add "$file"
            files_to_commit+=("$file")
        done
        print_success "✅ Auto-added Go code and project files"
    fi

    # Ask about documentation and config files
    if [[ ${#user_choice_files[@]} -gt 0 ]]; then
        print_info "📝 Documentation and config files found:"
        for file in "${user_choice_files[@]}"; do
            if gum confirm "Include $file?"; then
                git add "$file"
                files_to_commit+=("$file")
                print_info "  ✅ Added: $file"
            else
                print_info "  ⏭️  Skipped: $file"
            fi
        done
    fi

    # Ask about unknown files
    if [[ ${#other_files[@]} -gt 0 ]]; then
        print_info "❔ Unknown file types found:"
        for file in "${other_files[@]}"; do
            local file_action
            file_action=$(gum choose --header "What to do with $file?" \
                "Include in commit" \
                "Skip this file" \
                "Add to .gitignore")

            case "$file_action" in
                "Include in commit")
                    git add "$file"
                    files_to_commit+=("$file")
                    print_info "  ✅ Added: $file"
                    ;;
                "Skip this file")
                    print_info "  ⏭️  Skipped: $file"
                    ;;
                "Add to .gitignore")
                    echo "$file" >> .gitignore
                    print_info "  🚫 Added to .gitignore: $file"
                    if gum confirm "Include .gitignore in this commit?"; then
                        git add .gitignore
                        files_to_commit+=(".gitignore")
                    fi
                    ;;
            esac
        done
    fi

    # Handle ignored files
    if [[ ${#ignore_files[@]} -gt 0 ]]; then
        local gitignore_additions=()
        for file in "${ignore_files[@]}"; do
            if ! grep -q "^${file}$" .gitignore 2>/dev/null; then
                gitignore_additions+=("$file")
            fi
        done

        if [[ ${#gitignore_additions[@]} -gt 0 ]]; then
            if gum confirm "Add ${#gitignore_additions[@]} ignored files to .gitignore?"; then
                printf '%s\n' "${gitignore_additions[@]}" >> .gitignore
                git add .gitignore
                files_to_commit+=(".gitignore")
                print_success "  ✅ Updated .gitignore"
            fi
        fi
    fi

    # Commit if we have files
    if [[ ${#files_to_commit[@]} -gt 0 ]]; then
        echo
        print_info "📦 Files staged for commit:"
        printf '%s\n' "${files_to_commit[@]}" | sed 's/^/  /'
        echo

        # Generate smart commit message
        local suggested_message
        if [[ ${#auto_commit_files[@]} -gt 0 ]]; then
            suggested_message="Update Go code and project files"
        else
            suggested_message="Update project files"
        fi

        local commit_message
        commit_message=$(gum input --prompt "Commit message: " --value "$suggested_message")

        if [[ -n "$commit_message" ]]; then
            if git commit -m "$commit_message"; then
                print_success "✅ Changes committed successfully"

                # Push the commit to remote
                local current_branch
                current_branch=$(git rev-parse --abbrev-ref HEAD)
                if git push origin "$current_branch"; then
                    print_success "✅ Pushed changes to origin/$current_branch"
                else
                    print_warning "Failed to push to origin/$current_branch - you may need to push manually"
                fi
            else
                print_error "Failed to commit changes"
            fi
        else
            print_warning "Empty commit message, skipping commit"
        fi
    else
        print_info "No files selected for commit"
    fi
}

# Interactive commit workflow
interactive_commit_workflow() {
    print_header "Interactive Commit Workflow"

    while true; do
        # Show current status
        print_info "Current changes:"
        git status --short | sed 's/^/  /' || true
        echo

        if [[ "$HAS_GUM" == "true" ]]; then
            local commit_action
            commit_action=$(gum choose --header "What would you like to do?" \
                "Add all changes and commit" \
                "Select files to add" \
                "View detailed diff" \
                "Skip files (add to .gitignore)" \
                "Commit staged changes" \
                "Done - continue with release" \
                "Cancel release")

            case "$commit_action" in
                "Add all changes and commit")
                    git add .
                    commit_with_message
                    ;;
                "Select files to add")
                    select_files_to_add
                    ;;
                "View detailed diff")
                    view_detailed_diff
                    ;;
                "Skip files (add to .gitignore)")
                    add_to_gitignore
                    ;;
                "Commit staged changes")
                    if git diff --cached --quiet; then
                        print_warning "No staged changes to commit"
                    else
                        commit_with_message
                    fi
                    ;;
                "Done - continue with release")
                    if ! git diff --quiet || ! git diff --cached --quiet; then
                        if gum confirm "You still have uncommitted changes. Continue anyway?"; then
                            break
                        fi
                    else
                        print_success "All changes committed"
                        break
                    fi
                    ;;
                "Cancel release")
                    print_info "Release cancelled by user"
                    exit 0
                    ;;
            esac
        else
            break
        fi
    done
}

# Helper function to commit with a message
commit_with_message() {
    if git diff --cached --quiet; then
        print_warning "No staged changes to commit"
        return
    fi

    local commit_message
    if [[ "$HAS_GUM" == "true" ]]; then
        commit_message=$(gum input --prompt "Commit message: " --placeholder "Brief description of changes")
    else
        read -p "Commit message: " commit_message
    fi

    if [[ -n "$commit_message" ]]; then
        if git commit -m "$commit_message"; then
            print_success "Changes committed successfully"

            # Push the commit to remote
            local current_branch
            current_branch=$(git rev-parse --abbrev-ref HEAD)
            if git push origin "$current_branch"; then
                print_success "Pushed changes to origin/$current_branch"
            else
                print_warning "Failed to push to origin/$current_branch - you may need to push manually"
            fi
        else
            print_error "Failed to commit changes"
        fi
    else
        print_warning "Empty commit message, skipping commit"
    fi
}

# Helper function to select files to add
select_files_to_add() {
    local untracked_files modified_files deleted_files
    untracked_files=$(git ls-files --others --exclude-standard)
    modified_files=$(git diff --name-only)
    deleted_files=$(git diff --name-only --diff-filter=D)

    local all_files=""
    [[ -n "$untracked_files" ]] && all_files+="$untracked_files"$'\n'
    [[ -n "$modified_files" ]] && all_files+="$modified_files"$'\n'
    [[ -n "$deleted_files" ]] && all_files+="$deleted_files"$'\n'

    if [[ -z "$all_files" ]]; then
        print_info "No files to add"
        return
    fi

    if [[ "$HAS_GUM" == "true" ]]; then
        local selected_files
        selected_files=$(echo "$all_files" | grep -v '^$' | gum choose --no-limit --header "Select files to add:")

        if [[ -n "$selected_files" ]]; then
            echo "$selected_files" | while read -r file; do
                if [[ -n "$file" ]]; then
                    git add "$file"
                    print_info "Added: $file"
                fi
            done
        fi
    fi
}

# Helper function to view detailed diff
view_detailed_diff() {
    if [[ "$HAS_GUM" == "true" ]]; then
        local diff_type
        diff_type=$(gum choose --header "Which diff would you like to see?" \
            "Working directory changes" \
            "Staged changes" \
            "Specific file diff")

        case "$diff_type" in
            "Working directory changes")
                git diff | gum pager || git diff
            ;;
            "Staged changes")
                git diff --cached | gum pager || git diff --cached
                ;;
            "Specific file diff")
                local files
                files=$(git diff --name-only && git diff --cached --name-only)
                if [[ -n "$files" ]]; then
                    local selected_file
                    selected_file=$(echo "$files" | sort -u | gum choose --header "Select file to view:")
                    if [[ -n "$selected_file" ]]; then
                        git diff "$selected_file" | gum pager || git diff "$selected_file"
                    fi
                fi
                ;;
        esac
    fi
}

# Helper function to add files to .gitignore
add_to_gitignore() {
    local untracked_files
    untracked_files=$(git ls-files --others --exclude-standard)

    if [[ -z "$untracked_files" ]]; then
        print_info "No untracked files to ignore"
        return
    fi

    if [[ "$HAS_GUM" == "true" ]]; then
        local files_to_ignore
        files_to_ignore=$(echo "$untracked_files" | gum choose --no-limit --header "Select files to add to .gitignore:")

        if [[ -n "$files_to_ignore" ]]; then
            echo "$files_to_ignore" >> .gitignore
            print_success "Added files to .gitignore"

            if gum confirm "Add .gitignore to git?"; then
                git add .gitignore
            fi
        fi
    fi
}


# Main function
main() {
    local release_type="${1:-}"
    local custom_message="${2:-}"

    # Show usage if no arguments and not in interactive mode
    if [[ $# -eq 0 ]] && [[ "$HAS_GUM" != "true" ]]; then
        echo "Usage: $SCRIPT_NAME <patch|minor|major|vX.Y.Z> [custom_message]"
        echo ""
        echo "Examples:"
        echo "  $SCRIPT_NAME patch                     # Auto-detect patch release"
        echo "  $SCRIPT_NAME minor                     # Auto-detect minor release"
        echo "  $SCRIPT_NAME major                     # Auto-detect major release"
        echo "  $SCRIPT_NAME v1.2.3 \"Custom message\"  # Specific version"
        echo ""
        echo "Interactive mode (with gum installed):"
        echo "  $SCRIPT_NAME                           # Interactive release wizard"
        exit 1
    fi

    # Header
    if [[ "$HAS_GUM" == "true" ]]; then
        gum style --border double --margin "1 2" --padding "1 2" \
            --foreground 46 --border-foreground 46 \
            "🚀 Fast Local Release" \
            "Automated Release Process"
        echo
    else
        echo -e "${CYAN}${BOLD}"
        echo "╔════════════════════════════════════════╗"
        echo "║         Fast Local Release             ║"
        echo "║      Automated Release Process         ║"
        echo "╚════════════════════════════════════════╝"
        echo -e "${NC}"
    fi

    # Check prerequisites
    check_prerequisites

    # Determine initial release type
    local initial_release_type="$release_type"
    if [[ $# -eq 0 ]] && [[ "$HAS_GUM" == "true" ]]; then
        # Interactive mode - let user choose
        gum style --foreground 33 "🎯 Release Type Selection:"
        initial_release_type=$(gum choose --header "Choose release type:" \
            "patch" "minor" "major" "custom")
    elif [[ -z "$release_type" ]]; then
        print_error "Release type required. Use: patch, minor, major, or vX.Y.Z"
        exit 1
    fi

    # Run quick checks
    run_checks

    # Handle uncommitted changes
    handle_uncommitted_changes

    # Analyze recent changes
    analyze_recent_changes

    # Determine version
    local current_version suggested_version
    current_version=$(get_current_version)

    if [[ $initial_release_type =~ ^v?[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        # Specific version provided
        suggested_version="${initial_release_type#v}"  # Remove 'v' prefix
    else
        # Increment based on type
        suggested_version=$(increment_version "$current_version" "$initial_release_type")
    fi

    print_info "Current version: v$current_version"
    print_info "Suggested version: v$suggested_version"
    echo

    # Generate initial release notes
    local auto_release_notes
    if [[ -n "$custom_message" ]]; then
        auto_release_notes="$custom_message"
    else
        auto_release_notes=$(generate_release_notes "$initial_release_type" "$suggested_version")
    fi

    # Interactive configuration
    local config_result
    local is_interactive_mode="false"
    if [[ $# -eq 0 ]] && [[ "$HAS_GUM" == "true" ]]; then
        is_interactive_mode="true"
    fi
    config_result=$(configure_release "$initial_release_type" "$suggested_version" "$auto_release_notes" "$is_interactive_mode")

    # Parse configuration result
    IFS='|' read -r final_type final_version final_notes build_binaries update_homebrew is_dry_run <<< "$config_result"

    # Unescape newlines in notes
    final_notes="${final_notes//\\n/$'\n'}"

    # Ensure we have a valid version
    if [[ -z "$final_version" ]]; then
        final_version="$suggested_version"
    fi

    # Extract title from release notes (first line)
    local release_title
    release_title=$(echo -e "$final_notes" | head -1 | sed 's/^# //')

    if [[ "$is_dry_run" == "true" ]]; then
        print_header "🔍 Dry Run Mode"
        print_info "Release type: $final_type"
        print_info "Version: v$final_version"
        print_info "Title: $release_title"
        print_info "Build binaries: $build_binaries"
        print_info "Update Homebrew: $update_homebrew"
        echo
        if [[ "$HAS_GUM" == "true" ]]; then
            gum style --foreground 33 "📝 Release Notes Preview:"
            echo "$final_notes" | gum style --border normal --padding "1 2"
        else
            print_info "Release Notes Preview:"
            echo "$final_notes"
        fi
        print_success "Dry run complete - no actual release created"
        exit 0
    fi

    print_info "Final version: v$final_version"
    print_info "Release title: $release_title"

    # Build release binaries (if enabled)
    if [[ "$build_binaries" == "true" ]]; then
        build_release_binaries "$final_version"
    else
        print_info "Skipping binary builds as requested"
    fi

    # Create git tag
    create_git_tag "$final_version"

    # Create GitHub release with assets
    local release_url
    release_url=$(create_github_release "$final_version" "$release_title" "$final_notes")

    # Update Homebrew tap (if enabled and applicable)
    if [[ "$update_homebrew" == "true" ]]; then
        update_homebrew_tap "$final_version"
    else
        print_info "Skipping Homebrew tap update as requested"
    fi

    # Verify release
    verify_release "$final_version"

    # Success message
    echo
    if [[ "$HAS_GUM" == "true" ]]; then
        gum style --border double --margin "1 2" --padding "1 2" \
            --foreground 46 --border-foreground 46 \
            "🎉 Release Complete!" \
            "Version: v${final_version}" \
            "GitHub Release: ${release_url}"
    else
        echo -e "\n${GREEN}${BOLD}🎉 Release Complete!${NC}"
        echo -e "${GREEN}Version: v${final_version}${NC}"
        echo -e "${GREEN}GitHub Release: ${release_url}${NC}"
    fi

    # Show installation commands
    local repo_url github_user repo_name go_module_name homebrew_name
    repo_url=$(git remote get-url origin | sed 's/\.git$//')
    github_user=$(echo "$repo_url" | sed -n 's|.*github\.com[:/]\([^/]*\)/.*|\1|p')
    repo_name=$(basename "$repo_url")

    # Get the Go module name from go.mod
    if [[ -f "go.mod" ]]; then
        go_module_name=$(grep -E '^module ' go.mod | cut -d' ' -f2)
    else
        go_module_name="$repo_url"
    fi

    # For Homebrew, try to detect the actual formula name
    homebrew_name="$repo_name"
    if [[ -d "$HOME/homebrew-tap" ]]; then
        # Check if there's a formula file that matches the Go module name
        local module_basename=$(basename "$go_module_name")
        if [[ -f "$HOME/homebrew-tap/Formula/${module_basename}.rb" ]]; then
            homebrew_name="$module_basename"
        fi
    fi

    echo
    if [[ "$HAS_GUM" == "true" ]]; then
        gum style --foreground 33 "📦 Installation Commands:"
        if [[ -f "go.mod" ]]; then
            # Use HTTPS URL format for go install
            local https_url="${repo_url#git@github.com:}"
            https_url="github.com/${https_url}"
            gum style "  Go: go install ${https_url}@v${final_version}"
        fi
        if [[ -d "$HOME/homebrew-tap" && -n "$github_user" ]]; then
            gum style "  Homebrew: brew install ${github_user}/tap/${homebrew_name}"
        fi
    else
        if [[ -f "go.mod" ]]; then
            # Use HTTPS URL format for go install
            local https_url="${repo_url#git@github.com:}"
            https_url="github.com/${https_url}"
            echo -e "${BLUE}Go Install: go install ${https_url}@v${final_version}${NC}"
        fi
        if [[ -d "$HOME/homebrew-tap" && -n "$github_user" ]]; then
            echo -e "${BLUE}Homebrew Install: brew install ${github_user}/tap/${homebrew_name}${NC}"
        fi
    fi
}

# Run main function with all arguments
main "$@"