.PHONY: build test test-integration vet fmt tidy run-daemon

build:
	go build -o bin/roundpend ./cmd/roundpend
	go build -o bin/roundpen ./cmd/roundpen

test:
	go test ./...

# Requires DATABASE_URL (or ROUNDPEN_TEST_DATABASE_URL). Optional Docker SSH:
#   ROUNDPEN_TEST_DOCKER_HOST=ssh://user@host
#   ROUNDPEN_TEST_REMOTE_MOUNT=/path/on/remote (writable by container)
test-integration:
	go test ./tests/integration/ -count=1 -timeout 10m -v

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

run-daemon: build
	./bin/roundpend
