VERSION ?= 0.1.3
GO ?= go

.PHONY: build install test clean desktop desktop-install desktop-test
build:
	$(GO) build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o bin/selphy ./cmd/selphy

install:
	$(GO) install -trimpath -ldflags "-s -w -X main.version=$(VERSION)" ./cmd/selphy

test:
	$(GO) vet ./...
	$(GO) test -race ./...

clean:
	rm -rf bin dist

desktop:
	scripts/build-desktop.sh

desktop-install:
	scripts/install-desktop.sh

desktop-test:
	$(GO) test -race ./internal/desktop
	cd desktop && go vet ./... && npm ci && npm test
