.PHONY: build build-windows build-installer build-all test test-race clean run lint fmt coverage install help

# Версия бинарника (из env или дефолт)
REVIZOR_VERSION ?= 0.1.0
LDFLAGS_VERSION = -X main.Version=$(REVIZOR_VERSION)

build:
	go build -ldflags "$(LDFLAGS_VERSION)" -o revizor ./cmd/revizor/

build-windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS_VERSION)" -o revizor.exe ./cmd/revizor/

build-installer: build-windows
	@echo "Building Windows installer..."
	cp revizor.exe cmd/installer/revizor.exe
	cp revizor.yaml.example cmd/installer/revizor.yaml
	cp README.md cmd/installer/README.md
	echo "" > cmd/installer/license.key
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS_VERSION)" -o revizor-installer.exe ./cmd/installer/
	rm -f cmd/installer/revizor.exe cmd/installer/revizor.yaml cmd/installer/license.key cmd/installer/README.md
	@echo "Installer built: revizor-installer.exe"

build-all:
	@echo "Building for all platforms..."
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS_VERSION)" -o build/revizor-linux-amd64 ./cmd/revizor/
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS_VERSION)" -o build/revizor-linux-arm64 ./cmd/revizor/
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS_VERSION)" -o build/revizor-darwin-amd64 ./cmd/revizor/
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS_VERSION)" -o build/revizor-darwin-arm64 ./cmd/revizor/
	GOOS=windows GOARCH=amd64 go build -ldflags "$(LDFLAGS_VERSION)" -o build/revizor-windows-amd64.exe ./cmd/revizor/
	@echo "All binaries in build/"

test:
	go test ./internal/... -v -count=1

test-race:
	go test ./internal/... -v -race -count=1

lint:
	go vet ./...

fmt:
	go fmt ./...

coverage:
	go test -coverprofile=coverage.out ./internal/...
	go tool cover -func=coverage.out

install: build
	cp revizor /usr/local/bin/revizor

clean:
	rm -f revizor revizor.exe revizor-installer.exe revizor.db coverage.out
	rm -rf build/

run: build
	./revizor serve

help:
	@echo "Revizor Makefile"
	@echo "  make build           — build binary (host platform)"
	@echo "  make build-windows   — cross-compile for Windows"
	@echo "  make build-installer — build Windows self-extracting installer"
	@echo "  make build-all       — cross-compile all platforms to build/"
	@echo "  make test            — run tests"
	@echo "  make test-race       — run tests with race detector"
	@echo "  make lint            — go vet"
	@echo "  make fmt             — go fmt"
	@echo "  make coverage        — test coverage report"
	@echo "  make install         — build and install to /usr/local/bin"
	@echo "  make run             — build and serve"
	@echo "  make clean           — remove binaries, db, coverage, build/"
