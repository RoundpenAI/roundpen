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
	&& touch internal/ui/dist/.gitkeep

# --- Go ---
FROM golang:1.25-bookworm AS gobuild
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/internal/ui/dist ./internal/ui/dist
RUN CGO_ENABLED=0 go build -o /out/roundpend ./cmd/roundpend \
	&& CGO_ENABLED=0 go build -o /out/roundpen ./cmd/roundpen

# --- Runtime ---
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
	&& mkdir -p /data
COPY --from=gobuild /out/roundpend /usr/local/bin/roundpend
COPY --from=gobuild /out/roundpen /usr/local/bin/roundpen
WORKDIR /data
EXPOSE 9527
ENTRYPOINT ["/usr/local/bin/roundpend"]
