VERSION_FILE := VERSION
VERSION := $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.1.0")
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(GIT_COMMIT)"

BINDIR := bin
INSTALLDIR := $(HOME)/.bin

BINARIES := sqlscore calibrate

.PHONY: all clean lint build build/full test install release release/patch release/minor release/major help

all: clean lint build test ## Run clean, lint, build, test

help: ## Show this help
	@grep -E '^[a-zA-Z_/.-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

# ─── Clean ────────────────────────────────────────────────────────────────────

clean: ## Remove and recreate bin/
	rm -rf $(BINDIR)
	mkdir -p $(BINDIR)

# ─── Lint ─────────────────────────────────────────────────────────────────────

lint: ## Run go vet and govulncheck
	go vet -v ./...
	@if command -v govulncheck >/dev/null 2>&1; then \
		govulncheck ./...; \
	else \
		echo "govulncheck not installed — install with: go install golang.org/x/vuln/cmd/govulncheck@latest"; \
	fi

# ─── Build ────────────────────────────────────────────────────────────────────

build: $(BINDIR)/sqlscore $(BINDIR)/pg_calibrate $(BINDIR)/mysql_calibrate ## Build binaries using existing weights

$(BINDIR)/sqlscore: $(shell find . -name '*.go' -not -path './calibrate/*') scorer/weights.json
	go build $(LDFLAGS) -o $(BINDIR)/sqlscore ./cmd/sqlscore

$(BINDIR)/pg_calibrate: $(shell find . -name '*.go') scorer/weights/postgresql.json
	go build $(LDFLAGS) -o $(BINDIR)/pg_calibrate ./cmd/pg_calibrate

$(BINDIR)/mysql_calibrate: $(shell find . -name '*.go') scorer/weights/mysql.json
	go build $(LDFLAGS) -o $(BINDIR)/mysql_calibrate ./cmd/mysql_calibrate

build/full: $(BINDIR)/pg_calibrate $(BINDIR)/mysql_calibrate ## Generate weights via calibration, then build sqlscore
	@echo "Running weight calibration (this may take hours)..."
	$(BINDIR)/pg_calibrate -output scorer/weights.json
	go build $(LDFLAGS) -o $(BINDIR)/sqlscore ./cmd/sqlscore
	@echo "Build complete with freshly calibrated weights."

# ─── Install ──────────────────────────────────────────────────────────────────

install: build ## Copy binaries from bin/ to ~/.bin
	mkdir -p $(INSTALLDIR)
	cp $(BINDIR)/sqlscore $(INSTALLDIR)/sqlscore
	cp $(BINDIR)/pg_calibrate $(INSTALLDIR)/calibrate
	@echo "Installed to $(INSTALLDIR)/"

# ─── Test ─────────────────────────────────────────────────────────────────────

test: test/unit test/integration test/e2e ## Run all tests (unit → integration → e2e)

test/unit: ## Run unit tests
	go test -count=1 -timeout 60s ./parser/... ./scorer/... ./calibrate/...

test/integration: ## Run integration tests (requires PostgreSQL for calibrate)
	go test -count=1 -timeout 120s -tags integration ./...

test/e2e: build ## Run end-to-end tests against built binaries
	go test -count=1 -timeout 120s ./cmd/sqlscore/...

# ─── Release ──────────────────────────────────────────────────────────────────

release: release/patch ## Bump patch version (default)

release/patch: ## Bump patch version (0.1.0 → 0.1.1)
	@$(call bump,patch)

release/minor: ## Bump minor version (0.1.0 → 0.2.0)
	@$(call bump,minor)

release/major: ## Bump major version (0.1.0 → 1.0.0)
	@$(call bump,major)

define bump
	$(eval CURRENT := $(shell cat $(VERSION_FILE) 2>/dev/null || echo "0.1.0"))
	$(eval MAJOR := $(shell echo $(CURRENT) | cut -d. -f1))
	$(eval MINOR := $(shell echo $(CURRENT) | cut -d. -f2))
	$(eval PATCH := $(shell echo $(CURRENT) | cut -d. -f3))
	$(eval NEW_VERSION := $(shell \
		if [ "$(1)" = "major" ]; then echo "$$(($(MAJOR)+1)).0.0"; \
		elif [ "$(1)" = "minor" ]; then echo "$(MAJOR).$$(($(MINOR)+1)).0"; \
		else echo "$(MAJOR).$(MINOR).$$(($(PATCH)+1))"; fi))
	@echo "$(CURRENT) → $(NEW_VERSION)"
	@echo "$(NEW_VERSION)" > $(VERSION_FILE)
	@git add $(VERSION_FILE)
	@git commit -m "release: v$(NEW_VERSION)"
	@git tag -a "v$(NEW_VERSION)" -m "Release v$(NEW_VERSION)"
	@echo "Tagged v$(NEW_VERSION) — push with: git push && git push --tags"
endef
