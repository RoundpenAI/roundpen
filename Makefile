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

# Kill leftover roundpend / Vite from make dev (frees :19000/:19001). pg0 stays up.
stop:
	@./scripts/dev-stop.sh

dev-check:
	@./scripts/dev-up.sh --check-only

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
	go test ./internal/template/... ./internal/api/platform/... ./internal/sandbox/... -count=1 -coverpkg=./internal/template/...,./internal/api/platform/...,./internal/sandbox/...

test-integration:
	go test ./tests/integration/ -count=1 -timeout 10m -v

test-e2e:
	go test ./tests/integration/ -run TestE2E_CodingAgentWorkflow -count=1 -v

# Console UI smoke (Playwright + uismoke-api). First run: cd web && npx playwright install chromium
test-ui:
	cd web && npm run test:e2e

# Mobile console (Flutter, mobile/). Separate toolchain: nothing here is needed
# to build or run the control plane. Do NOT debug with `flutter run -d chrome`
# — the WebSocket auth header is native-only, the browser drops it.
mobile-setup:
	cd mobile && flutter pub get

mobile-dev:
	cd mobile && flutter run

mobile-test:
	cd mobile && flutter test

mobile-analyze:
	cd mobile && flutter analyze

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

# Playwright driver (no browsers: the browser runs in the Browser env container).
browser-driver:
	go run ./cmd/browserdriver

# Docker Agent image (git/ssh/curl). Used by local dev / private overrides.
code-agent-image:
	docker build -t roundpen-code-agent:local images/code-agent

# End-user path: no Node on the host. UI is baked in the image build.
# Requires POSTGRES_PASSWORD in .env (no default); see .env.compose.example.
compose-up:
	docker compose up -d --build

compose-down:
	docker compose down

# fnOS app package (packaging source in deploy/fnos; needs the official fnpack,
# see deploy/fnos/README.md). The NAS pulls the published image; WITH_IMAGE=1
# builds and bundles it instead (offline installs).
VERSION ?= 0.1.0
PLATFORM ?= x86
WITH_IMAGE ?= 0

fpk:
	@VERSION=$(VERSION) PLATFORM=$(PLATFORM) WITH_IMAGE=$(WITH_IMAGE) ./scripts/build-fnos-fpk.sh
