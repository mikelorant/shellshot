export PKG_CONFIG_PATH := $(CURDIR)/build/libghostty/lib/pkgconfig
export CGO_ENABLED := 1

.PHONY: deps build test vet fmt emoji-asset clean distclean

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

# Rebuilds internal/render/emoji/assets/AppleColorEmoji-shellshot.ttf.zst from
# the published release font (needs network, uv and git; no Mac). See
# scripts/README.md.
emoji-asset:
	git submodule update --init --depth 1 third_party/apple-emoji-ttf
	PYTHONDONTWRITEBYTECODE=1 uv run --no-project --python 3.12 \
		--with fonttools==4.68.0 --with pillow==13.0.0 --with pyyaml==6.0.3 \
		--with uharfbuzz==0.57.0 --with imagequant==1.1.5 --with zstandard==0.25.0 \
		python scripts/build-apple-emoji-asset.py

clean:
	rm -rf bin

# Also removes the libghostty build output (rebuild with `make deps`).
distclean: clean
	rm -rf build
