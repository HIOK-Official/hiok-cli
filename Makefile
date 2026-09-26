VERSION ?= dev
BINARY  ?= build/hiok

.PHONY: build test fmt vet install clean ops

build:
	go build -ldflags "-X github.com/HIOK-Official/hiok-cli/internal/cli.Version=$(VERSION)" -o $(BINARY) ./cmd/hiok

test:
	go test ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

install: build
	install -m 0755 $(BINARY) $(HOME)/.local/bin/hiok

clean:
	rm -rf build

# Refresh the embedded operation list after regenerating the SDKs.
ops:
	cp ../hiok-sdk/operations.json internal/cli/ops/operations.json
