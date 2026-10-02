BINARY := ghprofile
PKG    := ./...

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'

.PHONY: build
build: ## Build the binary into ./bin
	go build -o bin/$(BINARY) ./cmd/$(BINARY)

.PHONY: test
test: ## Run tests with the race detector
	go test -race $(PKG)

.PHONY: cover
cover: ## Run tests and open a coverage report
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -html=coverage.out

.PHONY: vet
vet: ## Run go vet
	go vet $(PKG)

.PHONY: lint
lint: ## Run golangci-lint
	golangci-lint run

.PHONY: check
check: vet test ## Everything CI runs, minus lint

.PHONY: snapshot
snapshot: ## Build release archives locally, without publishing
	goreleaser release --snapshot --clean

.PHONY: release-check
release-check: ## Validate .goreleaser.yaml
	goreleaser check

.PHONY: clean
clean: ## Remove build output
	rm -rf bin dist coverage.out
