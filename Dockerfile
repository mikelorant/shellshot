# syntax=docker/dockerfile:1
#
# shellshot container image. Build from a checkout whose libghostty submodule is initialised:
#
#   git submodule update --init --depth 1 third_party/ghostty
#   docker build --build-arg VERSION="$(git describe --tags --always --dirty)" -t shellshot:dev .
#
# Works for linux/amd64 and linux/arm64 (each platform builds natively or under emulation).

# ---- build: zig -> libghostty-vt -> shellshot binary ----
FROM golang:1.27-bookworm AS build

ARG VERSION=dev

# Pinned zig (the submodule needs 0.17.x). sha256 values come from
# https://ziglang.org/download/index.json.
ARG ZIG_VERSION=0.17.1
ARG ZIG_SHA256_X86_64=3581863e3cbcfd0fce0efc682ce7e9e93da96668a977791d356a26d2a7e9b17a
ARG ZIG_SHA256_AARCH64=a1546d5646eb184437918d6635ca6b762a0e889eac09cef720ee7a1ed3fe7795

RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git pkg-config xz-utils \
 && rm -rf /var/lib/apt/lists/*

RUN set -eu; \
    arch="$(uname -m)"; \
    case "$arch" in \
      x86_64)  sha="$ZIG_SHA256_X86_64" ;; \
      aarch64) sha="$ZIG_SHA256_AARCH64" ;; \
      *) echo "unsupported architecture: $arch" >&2; exit 1 ;; \
    esac; \
    tarball="zig-$arch-linux-$ZIG_VERSION.tar.xz"; \
    curl -fsSL -o "/tmp/$tarball" "https://ziglang.org/download/$ZIG_VERSION/$tarball"; \
    echo "$sha  /tmp/$tarball" | sha256sum -c -; \
    mkdir /opt/zig; \
    tar -xJf "/tmp/$tarball" -C /opt/zig --strip-components=1; \
    rm "/tmp/$tarball"; \
    ln -s /opt/zig/zig /usr/local/bin/zig

WORKDIR /src

# libghostty-vt first, so Go source edits do not rebuild it. This is the command from
# scripts/build-libghostty.sh without the submodule update (the build context has no .git).
COPY third_party/ghostty third_party/ghostty
RUN test -f third_party/ghostty/build.zig \
 || { echo "third_party/ghostty is empty: run 'git submodule update --init --depth 1 third_party/ghostty' and build again" >&2; exit 1; }
RUN --mount=type=cache,target=/root/.cache/zig \
    cd third_party/ghostty \
 && zig build -Demit-lib-vt -Doptimize=ReleaseFast --prefix /src/build/libghostty

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
ENV CGO_ENABLED=1 PKG_CONFIG_PATH=/src/build/libghostty/lib/pkgconfig
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath \
      -ldflags "-s -w -X github.com/mikelorant/shellshot/internal/cli.Version=${VERSION}" \
      -o /out/shellshot ./cmd/shellshot

# ---- runtime: the binary links glibc dynamically; fonts are embedded ----
FROM debian:bookworm-slim

ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="shellshot" \
      org.opencontainers.image.source="https://github.com/mikelorant/shellshot" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

# bash comes with the base image; the binary only needs libc.
RUN useradd --uid 1000 --create-home --shell /bin/bash shellshot \
 && install -d -o shellshot -g shellshot /work

COPY --from=build /out/shellshot /usr/local/bin/shellshot

ENV TERM=xterm-256color LANG=C.UTF-8 SHELL=/bin/bash
USER shellshot
WORKDIR /work
ENTRYPOINT ["shellshot"]
CMD ["--help"]
