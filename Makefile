.PHONY: build clean run install vet fmt

BINARY := cli-sidecar
PKG := ./cmd/cli-sidecar

build:
	go build -o $(BINARY) $(PKG)

run: build
	./$(BINARY)

install:
	go install $(PKG)

clean:
	rm -f $(BINARY)
	rm -rf dist/

vet:
	go vet ./...

fmt:
	gofmt -w .
