.RECIPEPREFIX = >
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hexadecimil/hyprcage/internal/version.Version=$(VERSION)

.PHONY: build dist vet test fmt generate clean

build:
> CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o hyprcage ./cmd/hyprcage

# Release assets: one static binary per system and architecture, and their
# checksums: what install.sh, install.ps1 and the plugin launchers download.
dist:
> rm -rf dist && mkdir -p dist
> CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/hyprcage-linux-amd64 ./cmd/hyprcage
> CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/hyprcage-linux-arm64 ./cmd/hyprcage
> CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o dist/hyprcage-windows-amd64.exe ./cmd/hyprcage
> cd dist && sha256sum hyprcage-linux-amd64 hyprcage-linux-arm64 hyprcage-windows-amd64.exe > SHA256SUMS

vet:
> go vet ./...

test:
> go test ./...

fmt:
> gofmt -l -w cmd internal

generate:
> go generate ./internal/wl/

clean:
> rm -f hyprcage
