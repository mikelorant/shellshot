# Research: sandboxing / containerised execution for shellshot

Status: design brief. Only this file was written.

## 0. Evidence limits (read first)

- **No shell in this run.** Despite the task text, my toolset was file read/write plus web fetch/search. I could **not** run `which docker podman …`, `sandbox-exec`, `container --version`, or any `/tmp` experiment. Everything about THIS machine is **unverified**. Section 8 gives a ~5 minute probe script that closes that gap.
- I read `docs/ARCHITECTURE.md` (first 60 lines) and the first 150 lines of `docs/research/script-format.md`. `internal/pty/pty.go` does not exist at that path. I did **not** read `internal/pty/*`, `internal/session/*` or `internal/cli/*`, so the interface in §5 is written against the architecture doc (`pty.Process{Read,Write,Resize,Wait,Close}`, "spawn command in a PTY"), and field names are assumptions.
- The web-search tool returned unusable output. I relied on direct fetches (summarised by the fetch tool's answer mode, so quotes are paraphrase-grade) and on background knowledge. Background-knowledge claims are marked **(memory)**.
- GitHub metadata came from `api.github.com` on this run. Dates in the API are 2026-10/11, so they are current.

## 1. Summary and recommendation

**Yes, there is an idiomatic way, but not a generic Go library.** I found no maintained Go library that abstracts over bwrap / Seatbelt / docker / podman / apple-container and auto-picks. The closest "use whatever is available" product is Anthropic's **sandbox-runtime** (`srt`). It is TypeScript/npm (a library and a CLI), so for a Go binary it is an exec-able CLI, not a library. The Codex CLI uses the same pattern (Seatbelt on macOS, bwrap+seccomp on Linux) inside Rust.

**Recommendation: keep shellshot backend-agnostic and wrap the child's argv.** The PTY and libghostty stay on the host. Only the child command runs inside the boundary. `pty.Config` gets a prefix, and a small `internal/sandbox` package turns `(spec, argv)` into a new `(argv, env, cleanup)`. Ship:

1. A generic **template backend** (user-supplied prefix, e.g. `podman run --rm -it --network none -v {{.Workdir}}:/work -w /work IMG`). It covers docker, podman, nerdctl, apple `container`, lima, colima, `ssh`, and anything else, with no per-tool Go code.
2. Two **built-in profiles** that need no image: **bwrap** (Linux) and **Seatbelt/sandbox-exec** (macOS). This is the host-filesystem model: read-only host, writable scratch and workdir, network off.
3. Auto-detection, but **explicit opt-in** (`--sandbox auto|none|bwrap|seatbelt|<template>`). Default `none`. Never silently run unsandboxed when `--sandbox` was requested: fail closed.

Do **not** embed Docker SDK / containerd / libcontainer / testcontainers. They add a large dependency tree and a PTY-attach protocol, and they give no benefit over wrapping the CLI that already does the PTY work.

## 2. Backend landscape

PTY note that applies to every wrapper: shellshot allocates the PTY on the host and hands the slave to the wrapper process as its stdio. Wrappers that exec the target directly in the same session (bwrap, sandbox-exec, unshare, firejail, nsjail) inherit the slave as the controlling terminal with no extra work. Container CLIs (`docker/podman/nerdctl/container run -t`) detect that stdin is a TTY, allocate **their own** PTY inside the container and relay bytes plus resize through the daemon. Both work from shellshot's point of view.

| Backend | OS | Isolation | Startup | FS / image story | Network control | PTY via host slave | Availability | Licence |
|---|---|---|---|---|---|---|---|---|
| **bubblewrap `bwrap`** | Linux | namespaces (user/mount/pid/net/ipc/uts); shares kernel | ~ms | host FS via `--ro-bind`, `--tmpfs`, `--bind`; **no image** | `--unshare-net` = loopback only | yes; use `--new-session` unless TIOCSTI filtered (CVE-2017-5226) | needs unprivileged userns; Ubuntu 24.04+ AppArmor restriction can block it | LGPL-2.0+ **(memory)**; GitHub API reports NOASSERTION |
| `unshare` (util-linux) | Linux | namespaces, no FS policy | ~ms | manual pivot_root/mounts | `-n` | yes | ubiquitous | GPL |
| `firejail` | Linux | namespaces+seccomp, **setuid root** | ~10s ms | profiles over host FS | `--net=none` | yes **(memory)** | distro pkg | GPL-2.0 **(memory)** |
| `nsjail` | Linux | namespaces+seccomp+cgroups, proto-config | ~ms | host FS mounts; no image | yes | yes | build/pkg; [google/nsjail](https://github.com/google/nsjail) Apache-2.0, pushed 2026-10-28 | Apache-2.0 |
| **go-landlock** (+seccomp) | Linux ≥5.13 | LSM FS (and TCP since V4) rules for **current process** | ~0 | host FS allowlist; no mounts/pivots | TCP bind/connect only (no UDP before V10) | n/a (in-process) | kernel-dependent; BestEffort silently degrades | MIT |
| runc / crun / youki | Linux | OCI container (ns+cgroups+seccomp), rootless possible | ~50–200 ms **(memory)** | needs a **rootfs bundle** + `config.json` | netns config | `terminal:true` + `--console-socket` (fd over SCM_RIGHTS), or run foreground in a PTY | needs bundle prep | Apache-2.0 |
| systemd-nspawn | Linux | container; needs root typically | ~100s ms | rootfs dir/image | `--private-network` | yes, own PTY | systemd hosts | LGPL |
| gVisor `runsc` | Linux | user-space kernel (strong) | ~100s ms | OCI bundle or via docker/podman runtime | netns/own stack | via OCI | Linux only | Apache-2.0 |
| Kata / Firecracker | Linux (KVM) | micro-VM (strongest) | 100 ms–1 s | image/rootfs + kernel | virtual NIC | via runtime | needs KVM | Apache-2.0 |
| **docker / podman / nerdctl** | Linux native; macOS/Windows via VM | container (shared VM kernel on mac) | 0.3–1 s warm **(memory)** | **image required** (digest-pin-able) | `--network none` | `run -it` | podman rootless common on Linux | Apache-2.0 (docker engine, podman, nerdctl) |
| **Apple `container`** | macOS 26+, Apple silicon only | one lightweight VM per container (Virtualization.framework) | ~sub-second claimed **(memory)** | OCI images | `--network <name[,mac=,mtu=]>`; a `none` mode is **not documented** in the command reference I fetched | `-i`/`-t`: `container run -it ubuntu:latest /bin/bash` | maintainers only support macOS 26 | Apache-2.0; [apple/container](https://github.com/apple/container) pushed 2026-11-06 |
| Lima / Colima | macOS/Linux | Linux VM (VZ or QEMU) + docker/containerd inside | VM boot once (s), then container ms | images via nerdctl/docker inside VM; host dirs mounted (virtiofs/sshfs) | as the inner runtime | via `limactl shell`/`docker` | brew install | Lima Apache-2.0, Colima MIT |
| OrbStack, Docker Desktop, `podman machine` | macOS | VM + docker/podman API | as above | images | as docker | via docker CLI | commercial licensing for OrbStack/Docker Desktop **(memory)** | proprietary / mixed |
| **`sandbox-exec` (Seatbelt)** | macOS (any) | kernel MAC (SBPL profile), no namespace, no image | ~ms | **host FS** with deny/allow rules | `(deny network*)`, or allow localhost proxy only | yes: it just execs the child, stdio inherited | present on stock macOS; **man page says DEPRECATED**, App Sandbox is recommended | system tool |
| App Sandbox / entitlements | macOS | signed app bundles | n/a | n/a | n/a | not usable for arbitrary CLI children | n/a | n/a |
| Virtualization.framework VM (own) | macOS | full VM | s | disk image | virtual NIC | needs agent/serial | heavy | n/a |
| WSL2 | Windows | Linux VM; use the Linux backends | s | n/a | n/a | n/a | out of scope (ARCHITECTURE says Linux/Windows are out of scope) | n/a |

Rootless vs rootful: bwrap, Seatbelt, rootless podman, Apple `container` need no root. docker (daemon), nspawn, firejail (setuid) and often runc do.

Image-based vs host-FS: for **docs generation and demos** the host-FS model is attractive. The child needs the user's own tools (`go`, `git`, the CLI being demoed), so a read-only bind of `/` plus a scratch writable directory is more convenient than building images. For **reproducibility** images are better (§7).

## 3. Verified facts behind the table

1. **sandbox-runtime.** Package `@anthropic-ai/sandbox-runtime`, CLI `srt` and TS library (`SandboxManager`). macOS: `sandbox-exec` with dynamically generated Seatbelt profiles. Linux: bubblewrap with the network namespace removed; traffic reaches host-side HTTP/SOCKS proxies via Unix sockets relayed by `socat`. Policy in `~/.srt-settings.json` or `--settings`. Filesystem: reads allowed by default with `denyRead` / `allowRead`; writes denied by default with `allowWrite` / `denyWrite` (deny wins). Network: deny-all by default; `allowedDomains` (wildcards, optional port), `deniedDomains`. Linux needs bwrap ≥0.4.0 (0.5+ recommended), `socat`, `ripgrep`. Limits: no glob support for write rules on Linux; Ubuntu 24.04+ needs `kernel.apparmor_restrict_unprivileged_userns` disabled; nested userns blocked by default (breaks Chromium, rootless podman). Labelled "Beta Research Preview", APIs may change. Apache-2.0, pushed 2026-11-06, ~7k stars. **Support:** direct (README via fetch). **Confidence:** high. Source: <https://github.com/anthropic-experimental/sandbox-runtime> (API reports the repo as `anthropics/sandbox-runtime`).
2. **Codex CLI.** macOS: commands run via `sandbox-exec` with a `-p` profile. Linux: `bwrap` + `seccomp` by default. Modes `read-only`, `workspace-write`, `danger-full-access`. Network off by default. `.git`, `.codex`, `.agents`, `.aws` are read-only inside writable roots. WSL2 uses the Linux sandbox. It fails inside Docker if namespace/bwrap/seccomp ops are blocked. `codex sandbox macos|linux [CMD]` runs a command in the sandbox. **Support:** direct. <https://developers.openai.com/codex/agent-approvals-security>. **Confidence:** high. Lesson: two production agents converged on the same Seatbelt+bwrap pair.
3. **sandbox-exec.** Man page NAME line says "(DEPRECATED)" and recommends App Sandbox. Synopsis: `sandbox-exec [-f profile-file] [-n profile-name] [-p profile-string] [-D key=value ...] command [arguments ...]`. The man page gives no SBPL syntax. <https://keith.github.io/xcode-man-pages/sandbox-exec.1.html>. **Confidence:** high on deprecation. Whether it still works on macOS 26: not tested here, but sandbox-runtime and Codex both depend on it in 2026 (inference).
4. **bwrap.** README confirms `--ro-bind`, `--dev`, `--proc`, `--unshare-pid`, `--new-session` (CVE-2017-5226 / TIOCSTI), loopback-only netns, setuid mode removed (userns-only). Flags `--die-with-parent`, `--chdir`, `--clearenv`, `--setenv`, `--unshare-net`, `--unshare-all`, `--tmpfs` exist in the man page **(memory)**. The README fetch did not show them. <https://github.com/containers/bubblewrap>.
5. **Apple `container`.** macOS 26 on Apple silicon; older macOS unsupported; Swift; Apache-2.0; no Go bindings found on its README. Run flags: `-i/--interactive`, `-t/--tty`, `-v/--volume`, `--mount type=,source=,target=,readonly`, `--network`, `--no-dns`, `-c/--cpus`, `-m/--memory`, `-e/--env`, `--env-file`, `-w/--workdir`. <https://github.com/apple/container/blob/main/docs/command-reference.md>. Exit-code passthrough: **not verified**. The underlying Swift **Containerization** package ([apple/containerization](https://github.com/apple/containerization), Apache-2.0, pushed 2026-10-30) is Swift-only: no Go binding found.
6. **runc terminals.** With `terminal:true`, runc allocates the PTY in the container's `/dev/pts/ptmx`. Detached mode requires `--console-socket` and sends the master fd over a Unix socket (SCM_RIGHTS). No abstract sockets. stdout/stderr merge. `\n`→`\r\n` translation. Fails with `open /dev/tty: no such device or address` when no TTY is available (e.g. CI). Helpers: `containerd/go-runc`. <https://github.com/opencontainers/runc/blob/main/docs/terminals.md>. **Confidence:** high.
7. **Colima** ("Containers on Lima") MIT, runtimes docker/containerd/incus, vm types vz/krunkit. Colima README does not say that QEMU is supported (fetch tool did not find it; Colima has a QEMU backend **(memory)**). <https://github.com/abiosoft/colima>. **Lima** Apache-2.0, pushed 2026-11-06, ~22k stars.
8. **go-landlock.** `landlock.V5.BestEffort().RestrictPaths(landlock.RODirs(...), landlock.RWDirs(...))`. Restricts **the whole current process** (all threads) and sets no-new-privs. `BestEffort` succeeds even if Landlock is unsupported (silent degrade, a security-relevant footgun). Nonexistent paths error unless `IgnoreIfMissing`. TCP restriction from V4, UDP from V10. MIT, pushed 2026-11-01. <https://pkg.go.dev/github.com/landlock-lsm/go-landlock/landlock>. Consequence (inference): it cannot sandbox a child from the parent; you would need a re-exec helper (`shellshot __sandbox-exec …` calling `RestrictPaths` then `syscall.Exec`). Possible later for bwrap-less hosts, not milestone 1.
9. **testcontainers-go.** MIT, pushed 2026-11-07. `WithProvider` (since v0.39.0) says "Currently only docker or podman are supported". Described as "a wrapper around the Docker daemon". Designed for tests (Ryuk garbage collector), though `TerminateContainer` exists for non-test use. The docs I fetched show `Exec(ctx, cmd)` with no TTY/stdin option. `WithConfigModifier` exposes Docker's `Config` (so `Tty`/`OpenStdin` could be set), but interactive attach is not a documented feature. **Assessment (inference):** it abstracts Docker-compatible daemons only, adds a heavy dependency tree, a reaper container and no interactive-PTY story. Not suitable here.
10. **Docker SDK** (`github.com/docker/docker/client`, docs v28.5.2): `ContainerCreate`, `ContainerAttach` (TTY mode gives one non-multiplexed stream), `ContainerResize`, `ContainerExecResize`. Usable for a PTY but you would re-implement what `docker run -it` does, and it is Docker-API-only (podman has a compat socket **(memory)**). <https://pkg.go.dev/github.com/docker/docker/client>.

## 4. Go libraries assessment

| Library | Verdict for shellshot | Why |
|---|---|---|
| `os/exec` + CLI wrapping | **Use** | the CLIs already solve PTY relay, resize, signals, cleanup |
| sandbox-runtime (`srt`) | Optional external backend via template | TS/npm, needs Node; Beta; policy model is excellent. Don't vendor. Don't reimplement its proxy |
| `github.com/opencontainers/runc/libcontainer` | No | Linux-only, cgo/privileges, needs rootfs |
| `containerd/go-runc`, containerd client | No | needs daemon/bundles; no mac |
| `containers/podman` bindings (v5) | No | talks to podman socket; podman-only, heavy |
| Docker SDK / moby client | No (maybe later as one backend) | see finding 10 |
| testcontainers-go | No | see finding 9 |
| `go-landlock` | Maybe later (self-sandbox helper on Linux without bwrap) | whole-process scope, silent degrade |
| nsjail wrappers | No | nsjail itself is a CLI; wrap via template; no maintained Go multi-backend wrapper found |
| `opencontainers/runtime-spec` | No | only if we ever generate bundles |
| lima as a library | No | `limactl` CLI is enough; Lima's Go packages are internal-ish **(memory)** |
| `apple/containerization` | No | Swift only; use `container` CLI |
| dagger / earthly / devcontainers | No | build/CI orchestrators, not exec wrappers |
| Daytona / E2B / microsandbox | Out of scope | remote or microVM services/SDKs, not "whatever is installed" (not examined deeply in this run) |

Search for a generic multi-backend Go library: I did not find one (negative result from limited search; the search tool returned little). Treat as "none known".

## 5. Architecture

### 5.1 Where the boundary sits

```
host: shellshot ── pty master ──┐             libghostty-vt (host, outside)
                                │ slave = stdin/out/err
                      [prefix argv from sandbox.Runner]  ← bwrap … -- | sandbox-exec … | podman run -it …
                                └── child command (inside)
```

The PTY and the terminal emulator stay outside. This is the only design that works uniformly: bwrap/sandbox-exec inherit the slave directly; `docker/podman/container run -it` see a TTY on stdin and make their own, so shellshot's PTY talks to the CLI client. Resize is delivered by shellshot calling `TIOCSWINSZ` on the master. The kernel sends SIGWINCH to the foreground process group (the wrapper). The container CLIs forward that to the engine (docker and podman do so **(memory)**; Apple `container` is **unverified**). bwrap and sandbox-exec share the same TTY, so resize is automatic there. runc direct (not recommended) needs a console socket.

### 5.2 Consumer-defined interface

```go
// package sandbox — pure Go, no cgo, no deps
type Spec struct {
    Workdir   string            // host dir, mounted rw at same path (or /work)
    Cwd       string            // cwd for the child (inside)
    Env       map[string]string // final child env (already deterministic)
    ReadOnly  []string          // extra ro mounts
    Writable  []string          // extra rw mounts (e.g. output dir)
    Network   bool              // false by default
    CPUs      float64; MemoryMB int // best effort, ignored if backend can't
    Name      string            // container name for cleanup
}
type Runner interface {
    Name() string
    // Wrap returns the argv to hand to pty.Start, extra env for the *wrapper*,
    // and a cleanup func (kills named container, removes temp profile).
    Wrap(spec Spec, argv []string) (out []string, env []string, cleanup func(), err error)
}
func Detect(lookPath func(string) (string, error), goos string) []Runner // fake lookup in tests
func FromTemplate(tmpl string) (Runner, error)                          // text/template over Spec
```

`pty.Config` stays unchanged. `internal/cli` calls `Wrap` before building the pty config and `defer cleanup()`. `session` never learns about sandboxes.

### 5.3 Built-in command prefixes (sketch; verify flags before shipping)

- **bwrap:** `bwrap --die-with-parent --new-session --unshare-all [--share-net if allowed] --ro-bind / / --dev /dev --proc /proc --tmpfs /tmp --tmpfs $HOME --bind <Workdir> <Workdir> --chdir <Cwd> --clearenv --setenv K V… -- argv…`. Note `--ro-bind / /` plus `--tmpfs $HOME` hides secrets in `$HOME`. `--new-session` calls `setsid`, which would **drop the controlling TTY** (inference from the TIOCSTI doc: it is protective, and may stop job control/`/dev/tty` use in a shell). **Must test with an interactive shell**: this is the main PTY risk. Alternative: a seccomp TIOCSTI filter via `--seccomp` (sandbox-runtime ships pre-generated filters).
- **Seatbelt:** `sandbox-exec -f <tmp>/profile.sb -D WORKDIR=… -- argv…` with a generated profile: `(version 1) (deny default)` or `(allow default)` + `(deny file-write*)` + `(allow file-write* (subpath WORKDIR) (subpath "/private/tmp/…") (literal "/dev/tty") (regex #"^/dev/ttys[0-9]+") (literal "/dev/null"))` + `(deny network*)`. Profile written to a temp file; cleanup removes it. A deny-default profile for general shells is fiddly; copy the structure Codex/srt use rather than invent it. (SBPL details: **memory**, not verified in this run.)
- **Container CLIs (one template):**
  `{{.Engine}} run --rm -it --name {{.Name}} --network none -v {{.Workdir}}:/work -w /work -e TERM -e LANG … IMAGE -- argv…`. Use `--init` where supported (docker/podman) so PID 1 reaps and forwards signals **(memory)**. For Apple `container` the equivalent is `container run -it --rm …` (flags from the reference above; `--network none` unconfirmed).

### 5.4 Behaviour spec

| Concern | Rule |
|---|---|
| Exit code | wrapper's exit code = child's for bwrap, sandbox-exec, `docker/podman run` (**memory**; Apple `container` unverified). Keep the existing 128+sig mapping. A distinct code or message for "sandbox backend failed to start" would need sentinel detection, so say so in docs |
| Signals | send to the whole process group of the wrapper (`Setpgid` / PTY session). bwrap: `--die-with-parent`. Containers: SIGTERM to CLI client is proxied under `--sig-proxy` (docker default for non-TTY?) — **unverified**; cleanup must `docker kill NAME` using `--name`. Use a random per-run name |
| Resize | host `TIOCSWINSZ` → SIGWINCH → wrapper. Test each backend with `stty size` after `Resize` |
| Env | pass a **minimal deterministic** set: `TERM=xterm-256color`, `LANG=C.UTF-8` (or `en_US.UTF-8`), `HOME=<scratch>`, `TZ=UTC`, `PATH`, `COLUMNS/LINES` unset. For bwrap use `--clearenv --setenv`; for Seatbelt `env -i … ` after the prefix; for containers `-e` flags (the host env is not inherited anyway) |
| Cwd / mounts | workdir mounted rw at same absolute path (bwrap, Seatbelt) or `/work` (containers). Output dir for `.shot` is written by shellshot **on the host**, so it needs no mount |
| Network | off by default for sandboxed runs. Allow-list-by-domain is out of scope; point at sandbox-runtime if needed |
| Resource limits | containers: `--cpus/--memory/--pids-limit`. bwrap/Seatbelt: none. Document rather than fake with `ulimit`. Always keep the script `Timeout` as the wall-clock backstop |
| Cancel | cleanup = kill group, then `engine kill/rm -f NAME`, then delete temp profile. Must run on SIGINT/SIGTERM and panic |
| Fail closed | explicit `--sandbox X` with X missing → error, non-zero exit; `auto` finds nothing → error unless `--sandbox auto,none`-like fallback is spelled out |

### 5.5 How the script selects it

**Recommendation: CLI flag/env only; no `Set Sandbox` in scripts.** A script that carries its own isolation policy is a security trap (untrusted script can choose `none`) and is not portable (script written on mac, run in Linux CI). Precedence per script-format.md is CLI > script > default, which would let a script weaken a CLI choice only if it were allowed. Compromise if requested later: scripts may only write `Set SandboxProfile "name"` naming a profile resolved by the CLI config, and may never select `none`. `Set Command` / `Env` / `Cwd` continue to describe the **inside** command.

## 6. Heavier library integration vs argv wrapping

| | Argv wrapping (recommended) | Embedded SDK (docker/containerd/libcontainer) |
|---|---|---|
| Code | ~300–500 lines + tests | thousands; per-engine API differences |
| Backends | any CLI, via templates | one per SDK |
| PTY | solved by the CLI | re-implement attach/resize/demux |
| Deps / cgo | none | large module graphs; Linux-only for libcontainer |
| Cleanup | by name/process-group | richer (API) but per engine |
| Error detail | exit code + stderr text | structured |
| Reproducible policy | templates are data | code |

Argv wrapping loses structured errors and fine-grained lifecycle control; neither matters for this use case.

## 7. Reproducibility (beyond isolation)

- Isolation does not give reproducibility. Reproducibility comes from **pinned inputs**: image by digest (`IMG@sha256:…`), a lockfile'd toolchain (Nix flake / `nix develop --command`), and pre-baked tool installs. Network off removes the biggest drift source.
- Recordings are inherently not byte-identical (child output timing is real; see script-format.md). Deterministic environment (§5.4) makes screen state at marks reproducible.
- **Recommendation:** shellshot should *not* build images or manage Nix. "Bring your own image/profile": `--sandbox 'podman run … IMG@sha256:…'`. Docs show a Dockerfile example and a Nix example (`--sandbox 'nix develop path:. --command'` is a prefix template too, though not a sandbox by itself (**inference**)). Optionally record `sandbox.name` and the resolved argv prefix in the `.shot` header, as metadata only.

## 8. Probe script to run on this machine (not run by me)

```sh
for t in docker podman nerdctl lima limactl colima container sandbox-exec bwrap firejail runc crun youki unshare systemd-nspawn qemu-system-aarch64 nix; do
  p=$(command -v $t) && echo "$t: $p" || echo "$t: -"
done
sw_vers; uname -m
docker --version; podman --version; container --version; limactl --version
# Seatbelt works?
sandbox-exec -p '(version 1)(allow default)(deny network*)' /usr/bin/curl -sS -m 3 https://example.com; echo "rc=$?"   # expect failure
sandbox-exec -p '(version 1)(allow default)(deny file-write*)' /bin/sh -c 'touch /tmp/x'; echo "rc=$?"            # expect failure
# PTY inheritance with Seatbelt
script -q /dev/null sandbox-exec -p '(version 1)(allow default)' /bin/sh -c 'tty; stty size'
# container PTY
container system start && container run --rm -it alpine sh -c 'tty; stty size'   # macOS 26 only
docker run --rm -it alpine sh -c 'stty size'   # needs a real TTY: fails with "the input device is not a TTY" otherwise
```

## 9. Ranked options

1. **Template backend + built-in bwrap + Seatbelt, argv wrapping** (recommended). Best fit with "no Docker lock-in", tiny, covers both OSes; matches what Codex and sandbox-runtime do for their built-ins.
2. **Template-only** (no built-ins). Smallest; the user writes prefixes; ship documented recipes. Valid fallback if Seatbelt profile work proves flaky.
3. **Delegate to `srt`** as a recipe (`--sandbox 'srt --settings …'`): free domain allowlists; needs Node; Beta.
4. Embedded SDK (docker) — only if structured lifecycle is demanded later.
5. In-process go-landlock helper — Linux fallback without bwrap; later.

## 10. Milestone: small atomic tasks

1. `internal/sandbox`: `Spec`, `Runner`, `Wrap`, `Detect(lookPath, goos)` with fake lookup; table tests; pure Go.
2. Template runner: `text/template` over `Spec`, shell-word splitting, quoting tests, error on missing keys.
3. bwrap runner: argv builder, golden tests on argv only (no exec).
4. Seatbelt runner: profile generator + temp-file lifecycle, golden profile tests.
5. Cleanup/Signals: named-container kill in `cleanup`, process-group kill; unit test with a fake runner.
6. CLI: `--sandbox`, `--sandbox-net`, `--sandbox-mount`, wiring in `record`/`snap` only; precedence; fail-closed; docs in README/ARCHITECTURE.
7. Integration tests with `t.Skip` when the tool is absent: `stty size` after resize, exit code passthrough, write-denied outside workdir, network denied, shell job control (`bwrap --new-session` check).
8. Docs recipes: podman + digest, docker, Apple `container`, lima (`limactl shell`), srt, nix.

Do **not** build: image building, registry pulls, VM management, a network proxy / domain allowlist, resource-limit emulation, Windows backend, a plugin system, or `Set Sandbox` in scripts.

## 11. Risks

- `sandbox-exec` is officially DEPRECATED; behaviour could change in a future macOS. Mitigation: isolate in one file, test in CI on macOS.
- bwrap `--new-session` vs interactive shell controlling TTY (task 7 resolves).
- Ubuntu 24.04+ AppArmor blocks unprivileged userns by default (per srt README); bwrap may fail until configured. Containers inside Docker CI may block bwrap (Codex doc).
- Apple `container`: macOS 26 + Apple silicon only; flags and `--network none` unverified; young project.
- Docker-in-CI has no TTY (`runc` doc error pattern) — shellshot always supplies a PTY so this should be fine, but the wrapped CLI checks its own stdin.
- Licences: bwrap LGPL (exec'd, so no linking concern, **memory**); Docker Desktop/OrbStack terms (commercial) are a user concern, not ours.
- Security expectation: namespaces/Seatbelt are not VM-grade isolation. Docs must say "untrusted" means "mitigated", and recommend microVM/gVisor (via template) for hostile code.

## 12. Open questions for the user

1. Is the default `none` (opt-in) acceptable, or should CI mode default to sandbox?
2. Must sandboxed runs have the user's tools (host-FS model), or are images fine?
3. Is network-off the right default for demos that `git clone`/`curl`?
4. Is macOS Seatbelt (deprecated) acceptable as a built-in, or only templates on macOS?
5. Should the `.shot` header record sandbox metadata?
6. Any need for Linux/CI at all now (ARCHITECTURE says Linux out of scope)?

## 13. Not verified

Local tool inventory and versions; any experiment; Seatbelt operation on this macOS; Apple `container` install state, exit-code passthrough, `--network none`, SIGWINCH forwarding; docker/podman exit-code and signal details **(memory)**; bwrap flags beyond README; licences for bwrap/firejail/nspawn **(memory)**; Daytona, E2B, microsandbox, greywall internals; stars/commit dates for runc, podman, containerd, gVisor, Colima, nsjail others than those cited; existence of any Go multi-backend sandbox library beyond a cursory search; shellshot's actual `internal/pty` API.

## Sources

- Kept: sandbox-runtime README — https://github.com/anthropic-experimental/sandbox-runtime (policy model, mechanisms; Apache-2.0, pushed 2026-11-06 via GitHub API)
- Kept: Codex approvals/security — https://developers.openai.com/codex/agent-approvals-security
- Kept: sandbox-exec man page — https://keith.github.io/xcode-man-pages/sandbox-exec.1.html
- Kept: bubblewrap README — https://github.com/containers/bubblewrap
- Kept: apple/container README and command reference — https://github.com/apple/container ; https://github.com/apple/container/blob/main/docs/command-reference.md
- Kept: apple/containerization — https://github.com/apple/containerization
- Kept: runc terminals — https://github.com/opencontainers/runc/blob/main/docs/terminals.md
- Kept: go-landlock — https://pkg.go.dev/github.com/landlock-lsm/go-landlock/landlock
- Kept: testcontainers-go docs — https://golang.testcontainers.org/features/creating_container/ , /features/configuration/
- Kept: Docker client — https://pkg.go.dev/github.com/docker/docker/client
- Kept: Colima — https://github.com/abiosoft/colima ; Lima — https://github.com/lima-vm/lima
- Metadata via api.github.com for apple/container (Apache-2.0, pushed 2026-11-06, ~51k stars), go-landlock (MIT, 2026-11-01, 371), testcontainers-go (MIT, 2026-11-07, ~5k), bubblewrap (NOASSERTION, 2026-10-14, ~9.4k), nsjail (Apache-2.0, 2026-10-28), lima (Apache-2.0, 2026-11-06), containerization (Apache-2.0, 2026-10-30)
- Rejected: web-search results this run — returned no usable content.
