.PHONY: build test dist

## build: k2c for this machine
build:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o k2c ./cmd/k2c

test:
	go vet ./...
	go test -race ./...

## dist VERSION=v0.1.0: every platform's archive and SHA256SUMS in dist/
dist:
	./scripts/dist.sh $(VERSION)
