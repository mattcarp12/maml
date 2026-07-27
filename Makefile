.PHONY: all configure build clean test e2e e2efail tree run

BUILD_DIR := $(CURDIR)/build
BIN_DIR   := $(BUILD_DIR)/bin

# Default: ensure configured, then build
all: build

configure:
	cmake --preset default

build: configure
	cmake --build --preset default

clean:
	@echo "==> Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)

# Legacy Go test harness (remove once C++ tests replace these)
test: all
	@PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)" go test ./... -v -cover

e2e: all
	@export PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)"; \
	go test ./test/integration_test.go -v -cover && \
	go test ./test/compile_fail_test.go -v -cover

e2efail: all
	@PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)" go test ./test/compile_fail_test.go -v -cover

# Convenience: compile and run a single file
# Usage: make run FILE=hello.maml
run: all
	@if [ -z "$(FILE)" ]; then echo "Usage: make run FILE=path/to/file.maml"; exit 1; fi
	@$(BIN_DIR)/mamlc $(FILE) $(BUILD_DIR)/maml_run_tmp && $(BUILD_DIR)/maml_run_tmp

tree:
	tree -I '.zig-cache|zig-*|test|build|bin'