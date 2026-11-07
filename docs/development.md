# Development

## Build

The Go binary links libghostty-vt through cgo. libghostty is built from the pinned Git submodule
`third_party/ghostty` with zig only (no cmake, no Xcode project).

| Command | Effect |
|---|---|
| `make deps` | Runs `scripts/build-libghostty.sh`: initialises the submodule (`git submodule update --init --depth 1 third_party/ghostty`) and runs `zig build -Demit-lib-vt -Doptimize=ReleaseFast --prefix build/libghostty`. Safe to repeat; it reuses the zig cache |
| `make build` | `go build -o bin/shellshot ./cmd/shellshot` |
| `make test` | `make check-showcase`, then `go test ./...` |
| `make vet` | `go vet ./...` |
| `make examples` | Records and exports every `contrib/examples/*.sh` into `build/examples/` (fast speed, 80x24) |
| `make test-scripts` | Runs every `contrib/examples/scripts/*.script` with `--list` and for real (about a minute); not part of `make test` |
| `make noto-asset`, `make emoji-asset` | Rebuild the embedded colour emoji fonts; see `scripts/README.md`. Needs network, and `uv` for the Apple one |
| `make showcase` | Records the showcase demo and exports the README image `docs/assets/showcase.png` (on demand; see below) |
| `make check-showcase` | Fails if `docs/assets/showcase.png` is stale (see "Regenerating the README image") |
| `make test-examples` | Runs every `contrib/examples/*.sh` under dash, bash, sh and zsh (takes a few minutes); it checks exit status, terminal restore and Ctrl-C handling |
| `make check-examples` | Records every `contrib/examples/*.sh` at 80x24 with the real binary, follows the cursor through the raw output to find lines that auto-wrap, and lists every non-ASCII character the demos print, flagging those no embedded font has (they render blank). Takes about a minute; needs `bin/shellshot` (it builds it), libghostty and `uv`. Not part of `make test` |
| `make docker-build` | Builds the container image `shellshot:dev` (see "Container image") |
| `make docker-smoke` | Runs that image: version, a `record -c`, an `export` to PNG, and `file` on the result |
| `make fmt` | `gofmt -w .` |
| `make clean` | Removes `bin/` and `dist/` |
| `make distclean` | Removes `bin/`, `dist/` and `build/`; run `make deps` again afterwards |

Requirements: Go 1.26 or newer with cgo, a C toolchain, `git`, `pkg-config`, and zig 0.17.x (the
submodule's `build.zig.zon` sets `minimum_zig_version`). The first `make deps` downloads zig
dependencies and takes several minutes; later runs take about a minute or less.

`make deps` produces `build/libghostty/{include,lib/libghostty-vt.a,lib/pkgconfig/libghostty-vt-static.pc}`.
The Go binding finds it with pkg-config, so the Makefile exports
`PKG_CONFIG_PATH=$PWD/build/libghostty/lib/pkgconfig` and `CGO_ENABLED=1`. When you run `go`
directly, export the same:

```sh
export PKG_CONFIG_PATH=$PWD/build/libghostty/lib/pkgconfig CGO_ENABLED=1
go test ./internal/recording/...
```

`.gitmodules` lists a second submodule, `third_party/apple-emoji-ttf`. The build does not need it:
`make deps` initialises only `third_party/ghostty`.

## Tests

- `make test` runs everything. Run `go vet` (`make vet`) and `make build` before committing.
- Race detector: `go test -race ./...`.
- Tests run in parallel. Packages run side by side (`go test -p N`, default `GOMAXPROCS`), and
  the tests of `internal/cli`, `internal/recording` and `internal/export` call `t.Parallel()`
  (`go test -parallel N` caps them, default `GOMAXPROCS`). A parallel-safe test:
  - calls `t.Parallel()` first and uses nothing shared that it mutates: no package variables
    (`cli.Version`), no `t.Setenv`, `os.Setenv` or `t.Chdir` (these panic or race), no fixed
    ports, PTY names or file paths. Use `t.TempDir()` and pass absolute paths.
  - drives time with a fake clock. A real deadline must leave generous slack for a loaded
    machine. Measure delays from an event the test observes, not from when the test started.
  - does not measure allocation (`testing.AllocsPerRun`, `runtime.MemStats`): other tests
    allocate meanwhile, and `AllocsPerRun` panics in a parallel test.

  Tests that cannot follow these rules stay serial. The serial tests of a package run first, then
  the parallel ones together, so keep the serial ones cheap. To split a long loop, use
  `t.Run` subtests that call `t.Parallel()`; do not call `t.Fatal` from your own goroutines.
  `-cpu 1,4` checks that a package does not depend on `GOMAXPROCS`.
- Fuzz targets cover the format and script parsers. List them with `grep -rn 'func Fuzz' internal`.
  Run one for a short time; `-fuzzminimizetime 0` skips minimisation of new inputs:

  ```sh
  go test ./internal/recording/bin/varint -run '^$' -fuzz FuzzUvarint -fuzztime 30s -fuzzminimizetime 0
  ```

- Golden image tests live in `internal/render/raster` and `internal/render/sprite`. After an
  intentional rendering change, regenerate them and review the diff in `testdata/`:

  ```sh
  go test ./internal/render/raster ./internal/render/sprite -update
  ```

## Regenerating the README image

`make showcase` builds the binary, records `contrib/examples/showcase.sh` at 80x24 and speed
`normal` into `build/showcase.shot`, and exports it to `docs/assets/showcase.png` with every
look and timing flag pinned (`scripts/build-showcase.sh` has the list): `--scale 2 --emoji-set noto`
(licence-clean artwork), 60 fps, 1.25x speed, pauses capped at 1 s, 2 s final hold. That gives about
29 s and 1.7 MB. The README shows it at half its pixel width (736 of 1472), so retina screens get
sharp text. If you change `--scale` or `--font-size`, update the `width` in `README.md`.

It runs only on demand, never as part of `make build`, because the image is committed and a new
1.7 MB binary on every build would bloat the history. Regenerate it when the demo or the export
flags change, and commit it deliberately. The demo runs in real time, so two runs can differ byte for byte
(frame boundaries move by a few milliseconds) while looking the same.

`docs/assets/showcase.png.src` holds a sha256 of `showcase.sh`, `lib.sh` and the record and export
settings. `make check-showcase` recomputes it and fails with `showcase.png is stale: run make showcase`
when they differ; `make test` runs it too. It is a hash comparison, so it is fast and needs no libghostty.

## Container image

The `Dockerfile` builds shellshot in a container: it installs a pinned zig (sha256 checked), builds
libghostty-vt from the submodule, builds the Go binary, and copies it into a `debian:bookworm-slim`
image (about 130 MB) that runs as the non-root user `shellshot` (uid 1000) in `/work`. The image has
`bash` and `less` for demos; fonts are embedded in the binary.

Prerequisites: Docker with BuildKit, and the submodule in the build context (the build stops with a
message if `third_party/ghostty` is empty). The first build downloads zig dependencies and takes a
few minutes; later builds reuse cache mounts.

```sh
git submodule update --init --depth 1 third_party/ghostty
make docker-build     # docker build, tag shellshot:dev, VERSION from git describe
make docker-smoke     # version, record -c 'echo hi', export to PNG in a temp dir
```

Use it from the `docker-compose.yaml` at the repository root (files go to `./recordings`, which is
git-ignored). `record -c` and `export` need no `-t`; a bare `record` starts a shell and needs `-it`:

```sh
mkdir -p recordings
docker compose run --rm shellshot record -o demo.shot -c 'echo hello'
docker compose run --rm shellshot export demo.shot -o demo.png
docker compose run --rm -it shellshot record -o session.shot
docker compose run --rm demo      # profile `demo`: records and exports contrib/examples/showcase.sh
```

Ownership: Docker Desktop and OrbStack show bind-mounted files as yours. On Linux the container
writes as uid 1000; run with `SHELLSHOT_UID=$(id -u) SHELLSHOT_GID=$(id -g)` set to write as yourself.

Multi-arch (needs a buildx builder; the non-native platform builds under QEMU and is slow):

```sh
docker buildx build --platform linux/amd64,linux/arm64 \
  --build-arg VERSION="$(git describe --tags --always --dirty)" -t shellshot:dev .
```

The GoReleaser archives are a separate path: they ship the binary per platform, built outside the
container. The image build does not use them or `scripts/build-libghostty.sh`; it runs the same
`zig build` itself. Publishing images to a registry is not configured.

## Releases

[GoReleaser](https://goreleaser.com) (`.goreleaser.yaml`, version 2) builds one archive per target
into `dist/`: macOS and Linux, each for amd64 and arm64. Publishing is not configured yet.

Prerequisites: `goreleaser` (`brew install goreleaser`), zig 0.17.x, `git` with the submodule
(`make release-libs` initialises it), `pkg-config`, and a Mac with Xcode or the Command Line Tools
(the macOS links need the SDK, see below). No tag, network access or secrets are needed.

| Command | Effect |
|---|---|
| `make release-libs` | Builds libghostty-vt for the four targets with `scripts/build-libghostty.sh <zig-target>`, one zig invocation each, into `build/libghostty-{linux,darwin}-{amd64,arm64}`. The native `build/libghostty` is separate and unchanged. Takes a few minutes cold |
| `make release-check` | `goreleaser check` (validates `.goreleaser.yaml`) |
| `make snapshot` | `goreleaser release --snapshot --clean --skip=publish`: builds everything into `dist/` without a tag, the network or publishing; the version looks like `0.0.0-SNAPSHOT-<commit>` |

Run `make release-libs` once (and again after bumping the libghostty pin), then `make snapshot`.
`dist/` holds:

- `shellshot_<version>_<os>_<arch>.tar.gz` for `darwin_amd64`, `darwin_arm64`, `linux_amd64` and
  `linux_arm64`. Each contains `shellshot`, `README.md` and `NOTICE` (the repository has no
  `LICENSE` file, so none is shipped);
- `checksum.txt` (sha256 of the four archives);
- one directory per target with the bare binary (`dist/shellshot_linux_arm64_v8.0/shellshot`), plus
  goreleaser's `config.yaml`, `artifacts.json` and `metadata.json`.

How the builds work:

- Every target is cross-compiled from one machine with `zig cc` / `zig c++` (`-target
  <arch>-<os>`), cgo enabled, `-trimpath`, `-s -w` and the version injected into
  `internal/cli.Version`. `PKG_CONFIG_PATH` points at the matching `build/libghostty-<os>-<arch>`.
- libghostty is linked statically. The Linux binaries are dynamic against glibc (`libc`, `libresolv`,
  `libpthread`, `librt`) and are pinned to glibc 2.28 (`x86_64-linux-gnu.2.28`), so they run on
  distributions of that age or newer. They do not run on musl systems such as Alpine.
- zig has no macOS `libresolv`, so the darwin links use the SDK: `make snapshot` sets `SDKROOT` from
  `xcrun --show-sdk-path` unless you set it. Building the macOS archives therefore needs a Mac.
- `shellshot` is about 36 MB per binary (the fonts are embedded) and about 29 MB per archive.

Publishing: `release.disable: true` and `changelog.disable: true` in the config keep everything
local. To release from GitHub Actions later, run `make release-libs` on a macOS runner with zig 0.17
and goreleaser installed, then `goreleaser release --clean` on a tag push with `GITHUB_TOKEN` set,
after removing `release.disable` (and setting `release.github` if the repository differs).

## Worktrees

1. Work in a worktree under `.worktrees/<task>` (git-ignored) on branch `task/<task>`:
   `git worktree add .worktrees/<task> -b task/<task>`.
2. Give the worktree a libghostty build: run `make deps`, or copy `build/` from another checkout.
3. A copied build keeps the old absolute `prefix=` line in `build/libghostty/lib/pkgconfig/*.pc`. It
   still works while the original checkout exists; to detach it, fix the line with `sed`.
4. Commit in small steps with [Conventional Commits](https://www.conventionalcommits.org/)
   (`feat:`, `fix:`, `docs:`, `test:`, `refactor:`).
5. Before merging, run `make vet test build`.
6. Merge the branch into `main`, then remove the worktree with `git worktree remove`.

## Bumping the libghostty pin

`go.mitchellh.com/libghostty` is written against one libghostty-vt C API, so the submodule is pinned
to the commit the binding pins (`66af4c7`). The rolling `tip` release of libghostty no longer
matches, so prebuilt downloads do not work.

```sh
git -C third_party/ghostty fetch --depth 1 origin <commit>
git -C third_party/ghostty checkout <commit>
git add third_party/ghostty
go get go.mitchellh.com/libghostty@<version> && go mod tidy   # a version built for that commit
make deps && make vet test build
```

Check the new commit's `minimum_zig_version` in `third_party/ghostty/build.zig.zon` against your zig.
Commit the submodule pointer and `go.mod`/`go.sum` together. The key-encoder test in
`internal/vt/ghostty` asserts exact key bytes for the pinned commit; rerun it after a bump.

## Troubleshooting

- `Package libghostty-vt-static was not found in the pkg-config search path`: run `make deps`, and
  make sure `PKG_CONFIG_PATH` points at this checkout's `build/libghostty/lib/pkgconfig`. A plain
  `go build` does not set it.
- `zig not found (brew install zig)`: install zig and check that `zig version` prints 0.17.x.
- A build works in one worktree and fails in another with missing headers or libraries: the cgo
  build cache holds results from another worktree's `build/`. Run `go clean -cache` and check the
  `prefix=` lines in the `.pc` files.
- `third_party/ghostty` is empty: `git submodule update --init --depth 1 third_party/ghostty`.
- No colour in `replay` output: `--color auto` disables colour when stdout is not a terminal or
  `NO_COLOR` is set. Pass `--color always`.
