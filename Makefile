.PHONY: build build-shim build-all clean run coordinator install vet fmt test docker-shim

BINARY := cli-sidecar
SHIM := shim
PKG := ./cmd/cli-sidecar
SHIM_PKG := ./cmd/shim

build:
	go build -o $(BINARY) $(PKG)

build-shim:
	go build -o $(SHIM) $(SHIM_PKG)

build-all: build build-shim

run: build
	./$(BINARY)

coordinator: build
	./$(BINARY) coordinator

install:
	go install $(PKG)

clean:
	rm -f $(BINARY) $(SHIM)
	rm -rf dist/

vet:
	go vet ./...

fmt:
	gofmt -w .

test:
	go test ./...

docker-shim:
	docker build -f Dockerfile.shim -t cli-sidecar-shim:latest .
