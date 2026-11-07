export PKG_CONFIG_PATH := $(CURDIR)/build/libghostty/lib/pkgconfig
export CGO_ENABLED := 1

.PHONY: deps build test vet fmt clean distclean examples test-examples

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

clean:
	rm -rf bin

# Also removes the libghostty build output (rebuild with `make deps`).
distclean: clean
	rm -rf build
