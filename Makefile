.PHONY: fmt vet lint test coverage

fmt:
	go fmt ./...

vet:
	go vet ./...

lint:
	golangci-lint run

test:
	go test ./... -v

coverage:
	go test ./... -coverprofile=coverage.out

# =============================================
# E2E / Integration Tests
# =============================================

test-e2e:
	go test ./test/integration -v

test-e2e-short:
	go test ./test/integration -short -v

update-golden:
	UPDATE_GOLDEN=true go test ./test/integration -v

runtime: 
	cd runtime && zig build