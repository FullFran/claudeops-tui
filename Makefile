# claudeops — common developer tasks.
# Run `make help` to list targets.

BINARY := claudeops
PKG    := ./cmd/claudeops
# Tracked Go files only, so gofmt skips untracked tooling dirs like .claude/.
GO_FILES := $(shell git ls-files '*.go' 2>/dev/null)

.PHONY: help build install test race fmt fmt-check vet lint ci update-pricing

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary (pure Go, no CGO)
	CGO_ENABLED=0 go build -o $(BINARY) $(PKG)

install: ## Install the binary via go install
	go install $(PKG)

test: ## Run all tests
	go test ./...

race: ## Run all tests with the race detector
	go test -race ./...

fmt-check: ## Fail if any tracked Go file needs formatting
	@bad=$$(gofmt -l $(GO_FILES)); \
	if [ -n "$$bad" ]; then echo "$$bad"; echo "gofmt needed (run 'make fmt')"; exit 1; fi

vet: ## Run go vet
	go vet ./...

lint: ## Run golangci-lint (advisory — not yet enforced in CI)
	golangci-lint run

fmt: ## Format all Go files
	gofmt -w $(GO_FILES)

ci: fmt-check vet build race ## Run the same checks as the ci workflow
	-$(MAKE) lint

update-pricing: ## Refresh the embedded LiteLLM pricing snapshot
	./scripts/update-pricing.sh
