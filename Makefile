export PKG_CONFIG_PATH := $(CURDIR)/build/libghostty/lib/pkgconfig
export CGO_ENABLED := 1

.PHONY: deps build test vet fmt clean distclean noto-asset

deps:
	./scripts/build-libghostty.sh

build:
	go build -o bin/shellshot ./cmd/shellshot

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Re-fetches the pinned Noto Color Emoji font and checks its sha256 (no-op when
# the committed assets already match).
noto-asset:
	./scripts/fetch-noto-emoji.sh

clean:
	rm -rf bin

# Also removes the libghostty build output (rebuild with `make deps`).
distclean: clean
	rm -rf build
