# Multi-stage build: Node (UI) → Go (embed) → slim runtime.
# End users never install Node; they pull/run this image or use compose.

# --- UI ---
FROM node:22-bookworm AS ui
WORKDIR /src
COPY web/package.json web/package-lock.json ./web/
RUN cd web && npm ci
COPY web/ ./web/
RUN mkdir -p internal/ui/dist \
	&& cd web && npm run build \
	&& touch ../internal/ui/dist/.gitkeep

# --- Go ---
FROM golang:1.26-bookworm AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
COPY third_party/ ./third_party/
RUN go mod download
COPY . .
COPY --from=ui /src/internal/ui/dist ./internal/ui/dist
RUN CGO_ENABLED=0 go build -o /out/roundpend ./cmd/roundpend \
	&& CGO_ENABLED=0 go build -o /out/roundpen ./cmd/roundpen

# Bake the Playwright driver (playwright-core, no browsers) into /opt/playwright.
# Offline/intranet installs must not reach the npm registry / nodejs.org on
# first use, so the driver ships in the image. Browsers are NOT installed: the
# browser runs in the Browser env container and is attached over CDP.
# PLAYWRIGHT_NODEJS_PATH makes the installer reuse a preinstalled Node instead
# of downloading its own; this stage's Debian Node is build-time only — the
# runtime stage provides alpine's Node at the same path (a nodejs.org glibc
# binary would not run on alpine anyway).
RUN apt-get update \
	&& apt-get install -y --no-install-recommends nodejs \
	&& rm -rf /var/lib/apt/lists/* \
	&& PLAYWRIGHT_DRIVER_PATH=/opt/playwright PLAYWRIGHT_NODEJS_PATH=/usr/bin/node \
		go run ./cmd/browserdriver \
	&& test -f /opt/playwright/package/cli.js \
	&& test ! -e /opt/playwright/node

# --- Runtime ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata nodejs \
	&& mkdir -p /data
COPY --from=gobuild /out/roundpend /usr/local/bin/roundpend
COPY --from=gobuild /out/roundpen /usr/local/bin/roundpen
# Baked driver from the gobuild stage: first use must not download anything.
COPY --from=gobuild /opt/playwright /opt/playwright
ENV PLAYWRIGHT_DRIVER_PATH=/opt/playwright \
	PLAYWRIGHT_NODEJS_PATH=/usr/bin/node
WORKDIR /data
EXPOSE 9527
ENTRYPOINT ["/usr/local/bin/roundpend"]
