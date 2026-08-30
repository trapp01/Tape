BINARY := tape
PKG    := ./...
LDFLAGS := -X github.com/trapp01/tape/internal/cli.version=$(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build install test vet fmt lint run clean

build:
	go build -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/tape

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/tape

test:
	go test $(PKG)

vet:
	go vet $(PKG)

fmt:
	gofmt -l -w .

lint: vet
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed on:" && gofmt -l . && exit 1)

run: build
	./bin/$(BINARY) $(ARGS)

clean:
	rm -rf bin
