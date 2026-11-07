export PKG_CONFIG_PATH := $(CURDIR)/build/libghostty/lib/pkgconfig
export CGO_ENABLED := 1

.PHONY: deps build test vet fmt noto-asset emoji-asset examples showcase check-showcase test-examples test-scripts check-examples docker-build docker-smoke clean distclean

deps:
	./scripts/build-libghostty.sh

build:
	go build -o bin/shellshot ./cmd/shellshot

test: check-showcase
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

# Records and exports every contrib/examples/*.sh into build/examples/ (fast speed, 80x24).
examples: build
	@set -e; mkdir -p build/examples; \
	for f in contrib/examples/*.sh; do \
		n=$${f##*/}; n=$${n%.sh}; \
		[ "$$n" = lib ] && continue; \
		echo "== $$n"; \
		EXAMPLES_SPEED=fast ./bin/shellshot record -o build/examples/$$n.shot --cols 80 --rows 24 -c "sh ./$$f" >/dev/null; \
		./bin/shellshot export build/examples/$$n.shot -o build/examples/$$n.png; \
	done; \
	ls -l build/examples | awk 'NR > 1 { printf "%10d  %s\n", $$5, $$9 }'

# Records contrib/examples/showcase.sh and exports the README image docs/assets/showcase.png.
# On demand only: the result is committed deliberately (see docs/development.md).
showcase: build
	./scripts/build-showcase.sh

# Fails when docs/assets/showcase.png no longer matches the demo and export flags it was made from
# (a sha256 comparison, instant).
check-showcase:
	./scripts/build-showcase.sh --check

# Runs every example under dash, bash, sh and zsh on a pty (takes a few minutes).
test-examples:
	./scripts/test-examples.sh

# Runs every contrib/examples/scripts/*.script with --list and for real (about a minute).
test-scripts: build
	./scripts/test-scripts.sh

# Renders every example for real and fails on lines that wrap past column 80 and on characters no
# embedded font has (needs bin/shellshot, libghostty and uv; takes a minute or two; not part of make test).
check-examples: build
	./scripts/check-examples-render.sh

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

# Builds the container image shellshot:dev (needs the ghostty submodule: see docs/development.md).
docker-build:
	docker build \
		--build-arg VERSION="$$(git describe --tags --always --dirty)" \
		--build-arg REVISION="$$(git rev-parse HEAD)" \
		-t shellshot:dev .

# Runs the image: prints the version, records a command, exports it to a PNG and checks the file.
docker-smoke:
	@set -e; dir=$$(mktemp -d); trap 'rm -rf "$$dir"' EXIT; \
	run="docker run --rm --user $$(id -u):$$(id -g) -v $$dir:/work shellshot:dev"; \
	$$run version; \
	$$run record -o smoke.shot -c 'echo hi'; \
	$$run export smoke.shot -o smoke.png; \
	file "$$dir/smoke.png" | grep -q 'PNG image data'; \
	file "$$dir/smoke.png"

clean:
	rm -rf bin

# Also removes the libghostty build output (rebuild with `make deps`).
distclean: clean
	rm -rf build
