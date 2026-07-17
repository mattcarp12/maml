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
	@go run meta/types/gen_types.go

	@echo "==> Running AST Codegen..."
	@go run meta/ast/gen_ast.go

	@echo "==> Running TAST Codegen..."
	@go run meta/tast/gen_tast.go

	@echo "==> Running HIR Codegen..."
	@go run meta/hir/gen_hir.go

	@echo "==> Running MIR Go Codegen..."
	@go run meta/mir/gen_mir_go.go

	@echo "==> Running MIR C++ Codegen..."
	@go run meta/mir/gen_mir_cpp.go

	@echo "==> Running ABI + Runtime Codegen..."
	@go run meta/abi/gen_abi.go

	@echo "==> Running go generate..."
	@go generate ./...

# 1. Build the Go Compiler Frontend
frontend: codegen
	@echo "==> Building Go Frontend..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/maml ./cmd/maml

# 2. Configure and Build the C++ LLVM Backend
backend: codegen
	@echo "==> Building C++ LLVM Backend with Ninja..."
	@mkdir -p $(BUILD_DIR)/backend
	@mkdir -p $(BIN_DIR)
	@cd $(BUILD_DIR)/backend && cmake -G Ninja -DCMAKE_BUILD_TYPE=Release ../../backend && ninja
	@cp $(BUILD_DIR)/backend/maml-backend $(BIN_DIR)/maml-backend

# 3. Build the C++ Runtime
runtime:
	@echo "==> Building C++ Runtime with Ninja (musl static)..."
	@mkdir -p $(BUILD_DIR)/runtime
	@cd $(BUILD_DIR)/runtime && cmake -G Ninja \
		-DCMAKE_C_COMPILER=clang \
		-DCMAKE_CXX_COMPILER=clang++ \
		-DCMAKE_C_FLAGS="--target=x86_64-linux-musl -static" \
		-DCMAKE_CXX_FLAGS="--target=x86_64-linux-musl -static" \
		-DCMAKE_BUILD_TYPE=Release \
		../../runtime && ninja
# 4. Clean Up Build Environments
clean:
	@echo "==> Cleaning all build artifacts..."
	@rm -rf $(BIN_DIR) $(BUILD_DIR)
	@rm maml_app
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
	@PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)" go test ./test/integration/compile_fail_test.go -v -cover

e2efail:
	@PATH="$(BIN_DIR):$$PATH" MAML_ROOT="$(CURDIR)" go test ./test/integration/compile_fail_test.go -v -cover

tree:
	tree -I '.zig-cache|zig-*|test|build|bin'