.PHONY: help setup fpk

help:
	@echo "用法: make <目标>"
	@echo ""
	@echo "开发"
	@echo "  setup              首次：.env、go mod、npm install"
	@echo "  dev                pg0 + air(roundpend :19001) + Vite :19000；改 Go 自动重启 API"
	@echo "  stop               停掉 make dev 留下的进程（air / roundpend / Vite）"
	@echo "  dev-check          只检查本机工具是否齐全"
	@echo ""
	@echo "构建"
	@echo "  build              Web UI + 本机 roundpend / roundpen"
	@echo "  build-ui           只构建并嵌入 Web UI"
	@echo "  build-go           只编 Go（internal/ui/dist 已有产物时）"
	@echo "  build-linux        带 UI 的 Linux amd64 二进制"
	@echo "  fpk                fnOS 包（依赖 setup；VERSION= PLATFORM=x86|arm）"
	@echo ""
	@echo "测试"
	@echo "  test               go test ./..."
	@echo "  test-template      模板 / 平台 / sandbox 覆盖"
	@echo "  test-integration   集成测试"
	@echo "  test-e2e           Coding agent 工作流"
	@echo "  test-ui            Playwright 控制台冒烟"
	@echo "  e2e-live           对已运行的 roundpend 做冒烟"
	@echo "  vet                go vet ./..."
	@echo "  fmt                gofmt -w ."
	@echo "  tidy               go mod tidy"
	@echo ""
	@echo "运行与镜像"
	@echo "  run-daemon         构建并运行 bin/roundpend"
	@echo "  browser-driver     安装 Playwright driver"
	@echo "  code-agent-image   构建本地 Agent 镜像"
	@echo "  compose-up         docker compose up -d --build"
	@echo "  compose-down       docker compose down"
	@echo ""
	@echo "移动端（需要 Flutter）"
	@echo "  mobile-setup       flutter pub get"
	@echo "  mobile-dev         flutter run"
	@echo "  mobile-test        flutter test"
	@echo "  mobile-analyze     flutter analyze"

setup:
	@echo "==> 检查 .env"
	@if [ ! -f .env ]; then cp .env.example .env; echo "已从 .env.example 创建 .env"; else echo ".env 已存在"; fi
	@echo "==> 下载 Go 模块"
	go mod download
	@echo "==> 安装 Web 依赖 (web/)"
	cd web && npm install --progress=true --no-audit
	@echo "==> 准备目录 bin、internal/ui/dist、data"
	mkdir -p bin internal/ui/dist data
	touch internal/ui/dist/.gitkeep
	@echo "Setup 完成。下一步: make dev"

# Local preview: tool check → pg0 → air(roundpend) + Vite (see scripts/dev-up.sh).
# Saving Go under cmd/ or internal/ rebuilds/restarts the API; Vite keeps HMR.
dev:
	@./scripts/dev-up.sh

# Kill leftover air / roundpend / Vite from make dev (frees :19000/:19001). pg0 stays up.
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

# Stamp the release into the binary (same VERSION as fpk). Override: VERSION=1.2.3 make build
VERSION_LDFLAGS = -X github.com/RoundpenAI/roundpen/internal/buildinfo.Version=$(VERSION)

build: build-ui
	go build -ldflags "$(VERSION_LDFLAGS)" -o bin/roundpend ./cmd/roundpend
	go build -ldflags "$(VERSION_LDFLAGS)" -o bin/roundpen ./cmd/roundpen

# Cross-compile Linux binary with UI embedded (for NAS / bare metal).
build-linux: build-ui
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(VERSION_LDFLAGS)" -o bin/roundpend-linux-amd64 ./cmd/roundpend
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "$(VERSION_LDFLAGS)" -o bin/roundpen-linux-amd64 ./cmd/roundpen

# Go-only rebuild when internal/ui/dist is already populated.
build-go:
	go build -ldflags "$(VERSION_LDFLAGS)" -o bin/roundpend ./cmd/roundpend
	go build -ldflags "$(VERSION_LDFLAGS)" -o bin/roundpen ./cmd/roundpen

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
# see deploy/fnos/README.md). Host roundpend + bundled pg0; no Docker image.
VERSION ?= 0.4.10
PLATFORM ?= x86

fpk: setup
	@VERSION=$(VERSION) PLATFORM=$(PLATFORM) ./scripts/build-fnos-fpk.sh
