.PHONY: build test vet lint check web web-deps web-check web-test e2e dist

# build embeds the web interface when it has been built (make web).
build:
	go build -o rflib ./cmd/rflib

test:
	go test ./...

vet:
	go vet ./...

lint:
	golangci-lint run ./...

check: vet lint test

# The web interface (web/) needs Node.js.
web-deps:
	cd web && npm ci --no-fund --no-audit

web: web-deps
	cd web && npm run build

web-check: web-deps
	cd web && npm run typecheck && npm test

# e2e builds everything and runs the browser smoke tests against rflib serve.
e2e: web build
	cd web && npm run browsers && npm run e2e

# dist builds the release archives for every system into dist/, with the web
# interface built in: make dist VERSION=v0.1.0. The release workflow runs it
# for each tag.
dist: web
	go run ./tools/dist -version $(VERSION)
