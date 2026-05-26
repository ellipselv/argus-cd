.PHONY: build test deb clean

VERSION ?= 0.1.0
BIN := bin/argus
DEB := dist/argus-cd_$(VERSION)_amd64.deb

build:
	mkdir -p bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o $(BIN) ./cmd/argus

test:
	go test -race ./...

deb: build
	mkdir -p dist
	VERSION=$(VERSION) nfpm pkg --packager deb --target $(DEB) --config nfpm.yaml

clean:
	rm -rf bin dist
