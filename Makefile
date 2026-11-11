.PHONY: build test dev clean dist release-check
# Pinned release tool; `go run` fetches it on first use.
GORELEASER = go run github.com/goreleaser/goreleaser/v2@v2.19.0
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
build:
	cd web && npm ci && npm run build
	mkdir -p bin
	go build -buildvcs=false -trimpath -ldflags "-X main.version=$(VERSION)" -o bin/agent-workbench ./cmd/agent-workbench
test:
	go test -race ./...
	cd web && npm run build
dev: build
	./bin/agent-workbench start
# Every platform's archive, the Linux packages and checksums, in dist/.
dist:
	$(GORELEASER) release --snapshot --clean
release-check:
	$(GORELEASER) check
clean:
	rm -rf bin dist web/dist/assets web/dist/index.html
