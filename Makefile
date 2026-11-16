.RECIPEPREFIX = >
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hexadecimil/offstage/internal/version.Version=$(VERSION)

.PHONY: build dist vet test fmt generate clean

build:
> CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o offstage ./cmd/offstage

# Release assets: one static binary per architecture and their checksums,
# what install.sh and the plugin launcher download.
dist:
> rm -rf dist && mkdir -p dist
> CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/offstage-linux-amd64 ./cmd/offstage
> CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/offstage-linux-arm64 ./cmd/offstage
> cd dist && sha256sum offstage-linux-amd64 offstage-linux-arm64 > SHA256SUMS

vet:
> go vet ./...

test:
> go test ./...

fmt:
> gofmt -l -w cmd internal

generate:
> go generate ./internal/wl/

clean:
> rm -f offstage
