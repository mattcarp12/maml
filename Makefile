# =============================================================================
# MAML Unified Build Pipeline Orchestration
# =============================================================================

.PHONY: all frontend backend runtime clean test test-e2e pipeline-demo codegen

# Directories
BIN_DIR     := $(CURDIR)/bin
BUILD_DIR   := $(CURDIR)/build
RUNTIME_DIR := $(CURDIR)/runtime

# Default target runs codegen before compiling the decentralized engine
all: codegen frontend backend runtime

# 0. Single Source of Truth Code Generation
codegen:
	@echo "==> Running Types Codegen..."
	@go run frontend/types/gen_types.go
	@echo "==> Running AST Codegen..."
	@go run frontend/ast/gen_ast.go
	@echo "==> Running TAST Codegen..."
	@go run frontend/tast/gen_tast.go
	@echo "==> Running HIR Codegen..."
	@go run frontend/hir/gen_hir.go
	@echo "==> Running MIR Go Codegen..."
	@go run frontend/mir/gen_mir.go
	@echo "==> Running MIR C++ Codegen..."
	@go run frontend/mir/gen_cpp_mir.go -dir=backend/include/mir/
	@echo "==> Running Runtime Codegen..."
	@go run tools/gen_runtime.go

# 1. Build the Go Compiler Frontend
frontend: codegen
	@echo "==> Building Go Frontend..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/maml ./cmd/maml/main.go

# 2. Configure and Build the C++ LLVM Backend
backend: codegen
	@echo "==> Building C++ LLVM Backend..."
	@mkdir -p $(BUILD_DIR)
	@mkdir -p $(BIN_DIR)
	@cd $(BUILD_DIR) && cmake -DCMAKE_BUILD_TYPE=Release ../backend && make
	@cp $(BUILD_DIR)/maml-backend $(BIN_DIR)/maml-backend

# 3. Build the Freestanding Zig Async Runtime
runtime:
	@echo "==> Building Zig Async Runtime..."
	@cd $(RUNTIME_DIR) && zig build 

# 4. Clean Up Build Environments
clean:
	@echo "==> Cleaning all build artifacts..."
	@rm -rf $(BIN_DIR) $(BUILD_DIR)
	@rm -rf $(RUNTIME_DIR)/zig-out $(RUNTIME_DIR)/.zig-cache
	@go clean ./...

# =============================================================================
# Testing & Quality Control
# =============================================================================
fmt:
	@go fmt ./...

vet:
	@go vet ./...

test: all
	@PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)" go test ./... -v -cover

e2e: all
	@PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)" go test ./test/integration/integration_test.go -v -cover

# =============================================================================
# The End-to-End Execution Pipeline (The Verification Loop)
# =============================================================================
# Usage: make run PROGRAM=test/programs/await1/await1.maml
PROGRAM ?= test/programs/await1/await1.maml

run: all
	@echo "==> [Step 1] Running Go Frontend (Parsing, Type-Checking, JSON Generation)..."
	@$(BIN_DIR)/maml compile $(PROGRAM) > $(BUILD_DIR)/ast.json

	@echo "==> [Step 2] Running C++ Backend (Consuming JSON, Injecting Attributes, Emitting LLVM IR)..."
	@$(BIN_DIR)/maml-backend $(BUILD_DIR)/ast.json > $(BUILD_DIR)/output.ll

	@echo "==> [Step 3] Invoking Clang (Running LLVM CoroSplit Pass & Linking with Zig Runtime)..."
	@clang++ -O2 $(BUILD_DIR)/output.ll \
		$(RUNTIME_DIR)/zig-out/lib/libmamlrt.a \
		-lpthread -ldl -o $(BIN_DIR)/maml_app

	@echo "==> [Step 4] Executing Final Compiled Native Concurrent Binary:"
	@echo "------------------------------------------------------------"
	@$(BIN_DIR)/maml_app

tree:
	tree -I '.zig-cache|zig-*|test|build|bin'