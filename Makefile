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