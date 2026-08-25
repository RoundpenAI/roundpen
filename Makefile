.PHONY: build test vet fmt tidy run-daemon

build:
	go build -o bin/roundpend ./cmd/roundpend
	go build -o bin/roundpen ./cmd/roundpen

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

run-daemon: build
	./bin/roundpend
