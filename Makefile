.PHONY: build test vet lint check

build:
	go build -o rflib ./cmd/rflib

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

check: vet lint test
