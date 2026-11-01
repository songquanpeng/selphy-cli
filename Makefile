VERSION ?= 0.1.2
GO ?= go

.PHONY: build install test clean
build:
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/selphy ./cmd/selphy

install:
	$(GO) install -trimpath -ldflags "-s -w -X main.version=$(VERSION)" ./cmd/selphy

test:
	$(GO) vet ./...
	$(GO) test -race ./...

clean:
	rm -rf bin dist
