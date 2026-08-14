# Frontend build
.PHONY: frontend-build frontend-dev frontend-lint

frontend-build:
	cd frontend && npm run build

frontend-dev:
	cd frontend && npm run dev

frontend-lint:
	cd frontend && npm run lint

# Backend build and run
.PHONY: backend-build backend-run backend-test

backend-build:
	cd backend && go build -o ../bin/cargobay ./cmd/server/main.go

backend-run: backend-build
	./bin/cargobay

backend-test:
	cd backend && go test -v ./pkg/... ./cmd/...

# Full build
.PHONY: build

build: backend-build frontend-build

# Clean
.PHONY: clean

clean:
	rm -rf bin/
	rm -rf frontend/dist/*

# Docker
.PHONY: docker docker-up docker-down

docker:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down
