# Development

## Build

The Go binary links libghostty-vt through cgo. libghostty is built from the pinned Git submodule
`third_party/ghostty` with zig only (no cmake, no Xcode project).

| Command | Effect |
|---|---|
| `make deps` | Runs `scripts/build-libghostty.sh`: initialises the submodule (`git submodule update --init --depth 1 third_party/ghostty`) and runs `zig build -Demit-lib-vt -Doptimize=ReleaseFast --prefix build/libghostty`. Safe to repeat; it reuses the zig cache |
| `make build` | `go build -o bin/shellshot ./cmd/shellshot` |
| `make test` | `go test ./...` |
| `make vet` | `go vet ./...` |
| `make fmt` | `gofmt -w .` |
| `make clean` | Removes `bin/` |
| `make distclean` | Removes `bin/` and `build/`; run `make deps` again afterwards |

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
