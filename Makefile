# claudeops — common developer tasks.
# Run `make help` to list targets.

BINARY := claudeops
PKG    := ./cmd/claudeops
# Tracked Go files only, so gofmt skips untracked tooling dirs like .claude/.
GO_FILES := $(shell git ls-files '*.go' 2>/dev/null)

.PHONY: help build install test race fmt fmt-check vet lint ci update-pricing \
        release-check snapshot version-check clean

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

lint: ## Run golangci-lint (enforced in CI)
	golangci-lint run

fmt: ## Format all Go files
	gofmt -w $(GO_FILES)

ci: fmt-check vet build race lint ## Run the same checks as the ci workflow

update-pricing: ## Refresh the embedded LiteLLM pricing snapshot
	./scripts/update-pricing.sh

# --- Release ----------------------------------------------------------------
# Publishing happens in GitHub Actions on a pushed tag. These targets exist so
# you can prove the release will work before creating that tag.

release-check: ## Validate .goreleaser.yaml
	goreleaser check

snapshot: ## Build all release artifacts locally without publishing
	goreleaser release --snapshot --clean
	@echo
	@echo "Artifacts in dist/:"
	@ls -1 dist/*.tar.gz dist/*.zip dist/checksums.txt 2>/dev/null || true

version-check: ## Verify a tag matches the version in the source tree (make version-check TAG=v0.14.0)
	@if [ -z "$(TAG)" ]; then echo "usage: make version-check TAG=v0.14.0"; exit 2; fi
	@want=$$(echo "$(TAG)" | sed 's/^v//;s/-.*//'); \
	got=$$(go run $(PKG) version | awk 'NR==1 {print $$2}'); \
	if [ "$$want" != "$$got" ]; then \
		echo "tag $(TAG) does not match source version $$got"; \
		echo "bump defaultVersion in internal/buildinfo/buildinfo.go first"; \
		exit 1; \
	fi; \
	echo "version $$got matches tag $(TAG)"

clean: ## Remove build artifacts
	rm -rf dist $(BINARY)
