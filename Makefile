.PHONY: setup build build-ui build-linux build-go test test-integration test-e2b-compat test-e2e e2e-live vet fmt tidy \
	dev dev-check install-kaniko run-daemon compose-up compose-down

setup:
	@echo "Initializing development environment..."
	@if [ ! -f .env ]; then cp .env.example .env; echo "Created .env from .env.example"; fi
	@go mod download
	@cd web && npm install
	@mkdir -p bin internal/ui/dist data
	@touch internal/ui/dist/.gitkeep
	@echo "Setup complete. Next: make dev"

# One-shot local preview: tool check → pg0 → roundpend + Vite (see scripts/dev-up.sh).
dev:
	@./scripts/dev-up.sh

dev-check:
	@./scripts/dev-up.sh --check-only

install-kaniko:
	@./scripts/install-kaniko.sh

# Node is only needed here (and in the Docker builder). End users run a
# prebuilt binary or `docker compose up` — they never need npm.
build-ui:
	@echo "Building Web UI..."
	@cd web && [ -d node_modules ] || npm ci
	@cd web && npm run build
	@mkdir -p internal/ui/dist
	@touch internal/ui/dist/.gitkeep

build: build-ui
	go build -o bin/roundpend ./cmd/roundpend
	go build -o bin/roundpen ./cmd/roundpen

# Cross-compile Linux binary with UI embedded (for NAS / bare metal).
build-linux: build-ui
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/roundpend-linux-amd64 ./cmd/roundpend
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/roundpen-linux-amd64 ./cmd/roundpen

# Go-only rebuild when internal/ui/dist is already populated.
build-go:
	go build -o bin/roundpend ./cmd/roundpend
	go build -o bin/roundpen ./cmd/roundpen

test:
	@mkdir -p internal/ui/dist && touch internal/ui/dist/.gitkeep
	go test ./...

test-template:
	@mkdir -p internal/ui/dist && touch internal/ui/dist/.gitkeep
	ROUNDPEN_TEST_DATABASE_URL="$${ROUNDPEN_TEST_DATABASE_URL:-postgres://roundpen:roundpen@127.0.0.1:5432/roundpen_test?sslmode=disable}" \
	go test ./internal/template/... ./internal/template/builder/... ./internal/api/e2b/... ./internal/sandbox/... -count=1 -coverpkg=./internal/template/...,./internal/template/builder/...,./internal/api/e2b/...,./internal/sandbox/...

test-integration:
	go test ./tests/integration/ -count=1 -timeout 10m -v

test-e2b-compat:
	go test ./tests/integration/ -run TestE2BCompatibility -count=1 -v

test-e2e:
	go test ./tests/integration/ -run TestE2E_CodingAgentWorkflow -count=1 -v

# Live smoke against a running roundpend (default BASE from .env ROUNDPEN_HTTP_ADDR).
e2e-live:
	@./scripts/e2e-coding-agent.sh

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

run-daemon: build
	./bin/roundpend

# End-user path: no Node on the host. UI is baked in the image build.
compose-up:
	docker compose up -d --build

compose-down:
	docker compose down
