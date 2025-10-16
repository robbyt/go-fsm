.PHONY: all
all: help

## help: Display this help message
.PHONY: help
help: Makefile
	@echo
	@echo " Choose a make command to run"
	@echo
	@sed -n 's/^##//p' $< | column -t -s ':' | sed -e 's/^/ /'
	@echo

## test: Run tests with race detection and coverage
.PHONY: test
test:
	go test -race -cover ./...

## coverage: Generate and display coverage report
.PHONY: coverage
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
	@echo
	@echo "To view HTML coverage report, run: go tool cover -html=coverage.out"

## bench: Run performance benchmarks
.PHONY: bench
bench:
	go test -run=^$$ -bench=. -benchmem ./...

## lint: Run golangci-lint code quality checks
.PHONY: lint
lint:
	go vet ./...
	golangci-lint run ./...

## lint-fix: Run golangci-lint with auto-fix for common issues
.PHONY: lint-fix
lint-fix:
	go vet ./...
	golangci-lint fmt ./...
	golangci-lint run --fix ./...

## example: Build the example binary
.PHONY: example
example:
	go build -o example/example ./example
