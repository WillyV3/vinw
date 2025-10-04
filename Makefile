.PHONY: all build clean test install run run-viewer

# Default target
all: build

# Build both binaries
build:
	@echo "Building vinw..."
	@go build -o vinw
	@echo "Building vinw-viewer..."
	@cd viewer && go build -o vinw-viewer

# Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -f vinw
	@rm -f viewer/vinw-viewer

# Run tests
test:
	@./test.sh

# Install to ~/.local/bin
install: build
	@./install.sh

# Run vinw
run: build
	@./vinw

# Run viewer
run-viewer: build
	@./viewer/vinw-viewer

# Development - run both in parallel (requires GNU parallel or two terminals)
dev:
	@echo "Start vinw in one terminal: make run"
	@echo "Start viewer in another terminal: make run-viewer"