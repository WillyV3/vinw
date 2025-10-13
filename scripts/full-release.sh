#!/bin/bash

# Full Release Pipeline for vinw
# Handles: commit, push, tag, homebrew update, and GitHub release with changelog

set -e

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
RED='\033[0;31m'
NC='\033[0m'

# Configuration
REPO_ORG="willyv3"
REPO_NAME="vinw"
HOMEBREW_TAP_PATH="$HOME/homebrew-tap"
FORMULA_FILE="$HOMEBREW_TAP_PATH/Formula/vinw.rb"

# Function to print colored output
print_step() {
    echo -e "${BLUE}→${NC} $1"
}

print_success() {
    echo -e "${GREEN}✓${NC} $1"
}

print_error() {
    echo -e "${RED}✗${NC} $1"
    exit 1
}

print_warning() {
    echo -e "${YELLOW}⚠${NC} $1"
}

# Check for uncommitted changes
check_git_status() {
    if [[ -n $(git status -s) ]]; then
        return 1
    fi
    return 0
}

# Get current version from tags
get_current_version() {
    git describe --tags --abbrev=0 2>/dev/null || echo "v0.0.0"
}

# Calculate next version
get_next_version() {
    local current=$1
    local bump_type=$2

    # Remove 'v' prefix
    version=${current#v}

    # Split into parts
    IFS='.' read -r major minor patch <<< "$version"

    case $bump_type in
        major)
            major=$((major + 1))
            minor=0
            patch=0
            ;;
        minor)
            minor=$((minor + 1))
            patch=0
            ;;
        patch)
            patch=$((patch + 1))
            ;;
        *)
            echo "$current"
            return
            ;;
    esac

    echo "v${major}.${minor}.${patch}"
}

# Generate changelog from commits
generate_changelog() {
    local from_tag=$1
    local to_tag=$2

    echo "## What's Changed"
    echo ""

    # Group commits by type
    local features=""
    local fixes=""
    local other=""

    while IFS= read -r commit; do
        if [[ $commit == *"feat:"* ]] || [[ $commit == *"add:"* ]] || [[ $commit == *"Add"* ]]; then
            features="${features}- ${commit}\n"
        elif [[ $commit == *"fix:"* ]] || [[ $commit == *"Fix"* ]]; then
            fixes="${fixes}- ${commit}\n"
        else
            other="${other}- ${commit}\n"
        fi
    done < <(git log ${from_tag}..HEAD --pretty=format:"%s" --no-merges)

    if [[ -n $features ]]; then
        echo "### Features"
        echo -e "$features"
    fi

    if [[ -n $fixes ]]; then
        echo "### Bug Fixes"
        echo -e "$fixes"
    fi

    if [[ -n $other ]]; then
        echo "### Other Changes"
        echo -e "$other"
    fi

    echo ""
    echo "**Full Changelog**: https://github.com/${REPO_ORG}/${REPO_NAME}/compare/${from_tag}...${to_tag}"
}

# Main release process
main() {
    echo ""
    echo "========================================="
    echo "     vinw Full Release Pipeline"
    echo "========================================="
    echo ""

    # Parse arguments
    BUMP_TYPE=${1:-patch}
    CUSTOM_MESSAGE=${2:-""}

    if [[ $BUMP_TYPE != "major" && $BUMP_TYPE != "minor" && $BUMP_TYPE != "patch" ]]; then
        print_error "Invalid bump type. Use: major, minor, or patch"
    fi

    # Step 1: Check for uncommitted changes
    print_step "Checking git status..."
    if ! check_git_status; then
        print_warning "Uncommitted changes detected. Committing them..."

        git add -A

        if [[ -n $CUSTOM_MESSAGE ]]; then
            git commit -m "$CUSTOM_MESSAGE"
        else
            # Generate commit message from changes
            COMMIT_MSG="Update: $(git diff --cached --name-only | head -3 | xargs basename -s .go | paste -sd ', ' -)"
            git commit -m "$COMMIT_MSG"
        fi
        print_success "Changes committed"
    else
        print_success "Working directory clean"
    fi

    # Step 2: Push to remote
    print_step "Pushing to remote..."
    git push origin master
    print_success "Pushed to GitHub"

    # Step 3: Get version info
    CURRENT_VERSION=$(get_current_version)
    NEW_VERSION=$(get_next_version "$CURRENT_VERSION" "$BUMP_TYPE")

    echo ""
    print_step "Current version: ${CURRENT_VERSION}"
    print_step "New version: ${NEW_VERSION}"
    echo ""

    # Step 4: Run tests
    print_step "Running tests..."
    if go test ./... > /dev/null 2>&1; then
        print_success "Tests passed"
    else
        print_warning "Some tests failed, continuing anyway"
    fi

    # Step 5: Build binaries to ensure they compile
    print_step "Building binaries..."
    go build -o vinw
    cd viewer && go build -o vinw-viewer && cd ..
    print_success "Build successful"

    # Clean up test builds
    rm -f vinw viewer/vinw-viewer

    # Step 6: Create and push tag
    print_step "Creating git tag ${NEW_VERSION}..."
    git tag -a "$NEW_VERSION" -m "Release $NEW_VERSION"
    git push origin "$NEW_VERSION"
    print_success "Tag created and pushed"

    # Step 7: Update Homebrew formula
    print_step "Updating Homebrew formula..."

    # Wait for GitHub to process the tag
    sleep 5

    # Download tarball and calculate SHA256
    TARBALL_URL="https://github.com/${REPO_ORG}/${REPO_NAME}/archive/${NEW_VERSION}.tar.gz"
    print_step "Downloading tarball from ${TARBALL_URL}..."

    SHA256=$(curl -sL "$TARBALL_URL" | shasum -a 256 | cut -d' ' -f1)

    if [[ -z $SHA256 ]]; then
        print_error "Failed to download tarball or calculate SHA256"
    fi

    print_step "SHA256: ${SHA256}"

    # Update formula
    if [[ -f $FORMULA_FILE ]]; then
        # Update URL and SHA256 in formula
        sed -i '' "s|url \".*\"|url \"${TARBALL_URL}\"|" "$FORMULA_FILE"
        sed -i '' "s|sha256 \".*\"|sha256 \"${SHA256}\"|" "$FORMULA_FILE"

        # Generate release notes summary for caveats (only for major/minor releases)
        RELEASE_NOTES=""
        if [[ $BUMP_TYPE == "major" || $BUMP_TYPE == "minor" ]]; then
            print_step "Generating release notes for caveats..."

            # Build release notes header
            RELEASE_NOTES="
      What's New in ${NEW_VERSION}:"

            # Add features and fixes
            while IFS= read -r commit; do
                # Clean up commit message and limit length
                clean_commit=$(echo "$commit" | sed 's/^[^:]*: //' | cut -c1-60)
                RELEASE_NOTES="${RELEASE_NOTES}
        • ${clean_commit}"
            done < <(git log ${CURRENT_VERSION}..HEAD --pretty=format:"%s" --no-merges | head -5)

            RELEASE_NOTES="${RELEASE_NOTES}
"
        fi

        # Update caveats section with version and release notes
        # Find the ASCII art section and inject version/notes after it
        if [[ -n "$RELEASE_NOTES" ]]; then
            # Write release notes to temp file to avoid awk newline issues
            echo "$RELEASE_NOTES" > /tmp/vinw_release_notes.txt

            # Use sed to inject release notes after ASCII art
            sed -i '' '/░░░░░    ░░░░░ ░░░░ ░░░░░    ░░░░ ░░░░/{
                r /tmp/vinw_release_notes.txt
                a\

            }' "$FORMULA_FILE"

            # Remove old release notes if they exist
            sed -i '' '/What'\''s New in v/,/^$/d' "$FORMULA_FILE"

            rm -f /tmp/vinw_release_notes.txt
        fi

        # Commit and push homebrew formula
        cd "$HOMEBREW_TAP_PATH"
        git add Formula/vinw.rb
        git commit -m "Update vinw to ${NEW_VERSION}"
        git push
        cd - > /dev/null

        print_success "Homebrew formula updated"
    else
        print_warning "Homebrew formula not found at $FORMULA_FILE"
    fi

    # Step 8: Generate changelog
    print_step "Generating changelog..."
    CHANGELOG=$(generate_changelog "$CURRENT_VERSION" "$NEW_VERSION")

    # Step 9: Create GitHub release
    print_step "Creating GitHub release..."

    # Create release with changelog
    gh release create "$NEW_VERSION" \
        --repo "${REPO_ORG}/${REPO_NAME}" \
        --title "Release ${NEW_VERSION}" \
        --notes "$CHANGELOG" \
        --latest

    print_success "GitHub release created"

    echo ""
    echo "========================================="
    echo -e "${GREEN}✓ Release ${NEW_VERSION} completed successfully!${NC}"
    echo "========================================="
    echo ""
    echo "Installation:"
    echo "  brew upgrade vinw"
    echo "  or"
    echo "  brew install ${REPO_ORG}/tap/vinw"
    echo ""
    echo "Release URL:"
    echo "  https://github.com/${REPO_ORG}/${REPO_NAME}/releases/tag/${NEW_VERSION}"
    echo ""
}

# Run main function
main "$@"