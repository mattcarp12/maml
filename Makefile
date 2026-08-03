.PHONY: all configure build clean test e2e e2efail tree run

BUILD_DIR := $(CURDIR)/build
BIN_DIR   := $(BUILD_DIR)/bin
TEST_RUNNER_BIN := $(BIN_DIR)/test_runner

# Default: ensure configured, then build
all: build

configure:
	cmake --preset debug

build: configure
	cmake --build --preset debug

clean:
	@echo "==> Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)

build-test-runner:
	@echo "==> Building C++ E2E test runner..."
	@mkdir -p $(BIN_DIR)
	clang++ -std=c++17 test/test_runner.cpp -o $(TEST_RUNNER_BIN)

e2e: all build-test-runner
	@echo "==> Running C++ E2E Tests..."
	@export PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)"; \
	cd test && $(TEST_RUNNER_BIN)

e2efail: all
	@PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)" go test ./test/compile_fail_test.go -v -cover

# Convenience: compile and run a single file
# Usage: make run FILE=hello.maml
run: all
	@if [ -z "$(FILE)" ]; then echo "Usage: make run FILE=path/to/file.maml"; exit 1; fi
	@$(BIN_DIR)/mamlc $(FILE) $(BUILD_DIR)/maml_run_tmp && $(BUILD_DIR)/maml_run_tmp

tree:
	tree -I '.zig-cache|zig-*|test|build|bin'