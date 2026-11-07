export PKG_CONFIG_PATH := $(CURDIR)/build/libghostty/lib/pkgconfig
export CGO_ENABLED := 1

.PHONY: deps build test vet fmt noto-asset emoji-asset examples test-examples clean distclean

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

# Records and exports every examples/*.sh into build/examples/ (fast speed, 80x24).
examples: build
	@set -e; mkdir -p build/examples; \
	for f in examples/*.sh; do \
		n=$${f##*/}; n=$${n%.sh}; \
		[ "$$n" = lib ] && continue; \
		echo "== $$n"; \
		EXAMPLES_SPEED=fast ./bin/shellshot record -o build/examples/$$n.shot --cols 80 --rows 24 -c "sh ./$$f" >/dev/null; \
		./bin/shellshot export build/examples/$$n.shot -o build/examples/$$n.png; \
	done; \
	ls -l build/examples | awk 'NR > 1 { printf "%10d  %s\n", $$5, $$9 }'

# Runs every example under dash, bash, sh and zsh on a pty (takes a few minutes).
test-examples:
	./scripts/test-examples.sh

# Re-fetches the pinned Noto Color Emoji font and checks its sha256 (no-op when
# the committed assets already match).
noto-asset:
	./scripts/fetch-noto-emoji.sh

# Rebuilds internal/render/emoji/assets/apple-emoji/AppleColorEmoji-shellshot.ttf.zst from
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
