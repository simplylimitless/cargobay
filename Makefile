# Cargobay Makefile
# Universal Binary Repository Manager

# Default target
.DEFAULT_GOAL := help

# Colors for output
BLUE := \033[36m
GREEN := \033[32m
YELLOW := \033[33m
RED := \033[31m
RESET := \033[0m

# =============================================================================
# Help
# =============================================================================

.PHONY: help
help: ## Display this help
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z_-]+:.*?##/ {printf "  ${BLUE}%-20s${RESET} %s\n", $$1, $$2}' $(MAKEFILE_LIST)

# =============================================================================
# Frontend
# =============================================================================

.PHONY: frontend-build frontend-dev frontend-lint frontend-test frontend-e2e

frontend-build: ## Build the frontend application
	@echo "${BLUE}Building frontend...${RESET}"
	cd frontend && npm run build

frontend-dev: ## Start the frontend development server
	@echo "${BLUE}Starting frontend development server...${RESET}"
	cd frontend && npm run dev

frontend-lint: ## Lint the frontend code
	@echo "${BLUE}Linting frontend...${RESET}"
	cd frontend && npm run lint

frontend-test: ## Run frontend unit tests
	@echo "${BLUE}Running frontend unit tests...${RESET}"
	cd frontend && npm test -- --watch=false

frontend-e2e: ## Run frontend E2E tests with Playwright
	@echo "${BLUE}Running frontend E2E tests...${RESET}"
	cd frontend && npx playwright test

frontend-e2e-ui: ## Run frontend E2E tests with UI
	@echo "${BLUE}Running frontend E2E tests (UI mode)...${RESET}"
	cd frontend && npx playwright test --ui

# =============================================================================
# Backend
# =============================================================================

.PHONY: backend-build backend-run backend-test backend-test-cover backend-generate

backend-build: ## Build the backend binary
	@echo "${BLUE}Building backend...${RESET}"
	cd backend && go build -o ../bin/cargobay ./cmd/server/main.go

backend-run: backend-build ## Run the backend server
	@echo "${BLUE}Running backend server...${RESET}"
	./bin/cargobay

backend-test: ## Run backend unit tests
	@echo "${BLUE}Running backend tests...${RESET}"
	cd backend && go test -v ./pkg/... ./cmd/...

backend-test-cover: ## Run backend tests with coverage
	@echo "${BLUE}Running backend tests with coverage...${RESET}"
	cd backend && go test -v -coverprofile=coverage.out ./pkg/... ./cmd/...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "${GREEN}Coverage report generated: coverage.html${RESET}"

backend-test-short: ## Run backend tests without output buffering
	@echo "${BLUE}Running backend tests (short output)...${RESET}"
	cd backend && go test -short ./pkg/... ./cmd/...

backend-test-package: ## Run tests for a specific package (use PKG=package/path)
	@echo "${BLUE}Running tests for $(PKG)...${RESET}"
	cd backend && go test -v ./$(PKG)/...

backend-generate: ## Generate mock files
	@echo "${BLUE}Generating mocks...${RESET}"
	@cd backend && go generate ./...

# =============================================================================
# Full Build
# =============================================================================

.PHONY: build dev clean

build: backend-build frontend-build ## Build both backend and frontend
	@echo "${GREEN}Build complete!${RESET}"

dev: ## Start development servers for both backend and frontend
	@echo "${BLUE}Starting development servers...${RESET}"
	@echo "${YELLOW}Backend: http://localhost:4500${RESET}"
	@echo "${YELLOW}Frontend: http://localhost:5173${RESET}"
	@echo "${YELLOW}API Proxy: http://localhost:4500/api${RESET}"
	@echo ""
	@echo "Run 'make docker-up' to start all services including database and cache"
	@echo "Then run 'make backend-run' in one terminal and 'make frontend-dev' in another"

# =============================================================================
# Testing
# =============================================================================

.PHONY: test test-all test-frontend test-backend

test: ## Run all tests (backend + frontend)
	@echo "${BLUE}Running all tests...${RESET}"
	$(MAKE) backend-test
	$(MAKE) frontend-test

test-all: ## Run all tests with coverage
	@echo "${BLUE}Running all tests with coverage...${RESET}"
	$(MAKE) backend-test-cover
	$(MAKE) frontend-test

test-backend: ## Run only backend tests
	$(MAKE) backend-test

test-frontend: ## Run only frontend tests
	$(MAKE) frontend-test

test-e2e: ## Run only E2E tests
	$(MAKE) frontend-e2e

# =============================================================================
# Docker
# =============================================================================

.PHONY: docker docker-up docker-down docker-logs docker-restart

docker: ## Build Docker images
	@echo "${BLUE}Building Docker images...${RESET}"
	docker compose build

docker-up: ## Start all services (postgres, redis, trivy, cargobay)
	@echo "${BLUE}Starting services...${RESET}"
	docker compose up -d
	@echo "${GREEN}Services started!${RESET}"
	@echo "  - cargobay: http://localhost:4500"
	@echo "  - postgres: localhost:5432"
	@echo "  - redis: localhost:6379"
	@echo "  - trivy: localhost:4999"

docker-down: ## Stop all services
	@echo "${BLUE}Stopping services...${RESET}"
	docker compose down

docker-logs: ## View logs for all services
	docker compose logs -f

docker-restart: ## Restart all services
	@echo "${BLUE}Restarting services...${RESET}"
	docker compose restart

docker-cleanup: ## Remove all containers, volumes, and networks
	@echo "${YELLOW}Cleaning up Docker resources...${RESET}"
	docker compose down -v --remove-orphans
	docker system prune -a --volumes -f

# =============================================================================
# Database
# =============================================================================

.PHONY: db-migrate db-migrate-status db-seed

# The server also applies pending migrations automatically on startup (see
# backend/pkg/migrate and backend/cmd/server/main.go), backing up first if
# the database already has data. These targets are for manual/CI use only.
db-migrate: ## Apply database migrations
	@echo "${BLUE}Applying database migrations...${RESET}"
	@cd backend && go run ./cmd/cli migrate up

db-migrate-status: ## List pending database migrations
	@cd backend && go run ./cmd/cli migrate status

db-seed: ## Seed database with initial data
	@echo "${BLUE}Seeding database...${RESET}"
	@cd backend && go run -tags seed seed/main.go

# =============================================================================
# Linting and Security
# =============================================================================

.PHONY: lint lint-all security

lint: ## Run all linting
	@echo "${BLUE}Running linting...${RESET}"
	$(MAKE) frontend-lint

lint-all: ## Run all linting and checks
	@echo "${BLUE}Running all linting and checks...${RESET}"
	$(MAKE) lint

security: ## Run security scan
	@echo "${BLUE}Running security scan...${RESET}"
	@cd backend && gosec ./...
	@echo "${GREEN}Security scan complete${RESET}"

# =============================================================================
# Utilities
# =============================================================================

.PHONY: format tidy generate

format: ## Format Go code
	@echo "${BLUE}Formatting Go code...${RESET}"
	@cd backend && gofmt -w ./pkg ./cmd

tidy: ## Tidy Go dependencies
	@echo "${BLUE}Tidying Go dependencies...${RESET}"
	@cd backend && go mod tidy

generate: ## Run code generation
	@echo "${BLUE}Running code generation...${RESET}"
	@cd backend && go generate ./...

# =============================================================================
# Clean
# =============================================================================

.PHONY: clean clean-all clean-frontend clean-backend clean-docker

clean: ## Clean build artifacts
	@echo "${BLUE}Cleaning build artifacts...${RESET}"
	rm -rf bin/
	rm -rf frontend/dist/*
	rm -f coverage.out coverage.html

clean-all: ## Clean everything including node modules
	@echo "${YELLOW}Cleaning everything...${RESET}"
	$(MAKE) clean
	rm -rf frontend/node_modules/
	rm -rf backend/pkg/proxy/*/mocks/
	rm -rf backend/pkg/*/*/mocks/

clean-frontend: ## Clean frontend build artifacts
	rm -rf frontend/dist/*
	rm -rf frontend/node_modules/.cache

clean-backend: ## Clean backend build artifacts
	rm -rf bin/
	rm -f coverage.out coverage.html
	cd backend && go clean -cache

clean-docker: ## Clean Docker resources
	docker compose down -v --remove-orphans

# =============================================================================
# Test Targets with Specific Packages
# =============================================================================

# Proxy tests
.PHONY: test-proxy test-npm test-maven test-docker test-pypi test-nuget test-helm

test-proxy: ## Run all proxy tests
	$(MAKE) test-package PKG=pkg/proxy

test-npm: ## Run NPM proxy tests
	$(MAKE) test-package PKG=pkg/proxy/npm

test-maven: ## Run Maven proxy tests
	$(MAKE) test-package PKG=pkg/proxy/maven

test-docker: ## Run Docker proxy tests
	$(MAKE) test-package PKG=pkg/proxy/docker

test-pypi: ## Run PyPI proxy tests
	$(MAKE) test-package PKG=pkg/proxy/pypi

test-nuget: ## Run NuGet proxy tests
	$(MAKE) test-package PKG=pkg/proxy/nuget

test-helm: ## Run Helm proxy tests
	$(MAKE) test-package PKG=pkg/proxy/helm

# Middleware tests
.PHONY: test-middleware

test-middleware: ## Run middleware tests
	$(MAKE) test-package PKG=pkg/middleware

# Auth tests
.PHONY: test-auth

test-auth: ## Run authentication tests
	$(MAKE) test-package PKG=pkg/auth

# RBAC tests
.PHONY: test-rbac

test-rbac: ## Run RBAC tests
	$(MAKE) test-package PKG=pkg/rbac

# Database tests
.PHONY: test-database

test-database: ## Run database tests
	$(MAKE) test-package PKG=pkg/database

# Cache tests
.PHONY: test-cache

test-cache: ## Run cache tests
	$(MAKE) test-package PKG=pkg/cache

# Storage tests
.PHONY: test-storage

test-storage: ## Run storage tests
	$(MAKE) test-package PKG=pkg/storage

# =============================================================================
# CI/CD Targets
# =============================================================================

.PHONY: ci ci-backend ci-frontend ci-test

ci: ## CI target - runs all checks
	@echo "${BLUE}Running CI checks...${RESET}"
	$(MAKE) lint
	$(MAKE) test-all

ci-backend: ## CI backend target
	$(MAKE) backend-test-cover

ci-frontend: ## CI frontend target
	$(MAKE) frontend-lint
	$(MAKE) frontend-test
	$(MAKE) frontend-e2e

ci-test: ## CI test target
	$(MAKE) test-all

# =============================================================================
# Help Text
# =============================================================================

# You can run specific test patterns
# Example: make test-package PKG="pkg/proxy/npm"
