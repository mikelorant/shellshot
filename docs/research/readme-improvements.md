# Research: improving the shellshot README using The Good Docs Project (TGDP)

Status: research only; README.md and other docs untouched. Read date: all web sources were fetched in this single run; the session gave me no clock, so I cannot print a calendar date. Please stamp "read on <date>" when filing. Sources are live pages and may change.

## Summary and recommendation

The TGDP README template is a loose checklist that says "reorder and remove sections". Our README already covers its core (description, install, run, troubleshoot, licence) and is stronger than the template on examples and expected output. The real gaps are:

1. **Install builds from source only.** Releases (tar.gz archives for darwin/linux × amd64/arm64, `checksum.txt`) and `ghcr.io/mikelorant/shellshot` now exist. Lead with them: that turns a roughly 6-command, multi-minute, zig-plus-Go-toolchain install into 1-2 commands.
2. **No "who it is for" line, no prerequisites list, no support/contributing pointer.** TGDP asks for all three. Each costs one line.
3. **"Use" is dense.** It mixes quickstart, how-to and reference prose (replay internals, exit codes, DEC 2026, loops). TGDP says quickstarts should avoid error scenarios and reference should limit procedure. Move detail to docs/; keep a copy-paste path with expected output in the README.
4. **Add a short CONTRIBUTING.md and a SECURITY.md.** Skip a hand-written CHANGELOG (the draft GitHub Release is generated from commits). Skip the template's Table of contents, project owner/URL, badges, "Additional documentation" and "How to get help" as separate sections.

Proposed result: about 150-165 lines, same top (logo, tagline, showcase), no project-structure section.

Where TGDP conflicts with the maintainer's rules, I follow the maintainer. TGDP suggests an "approachable, friendly voice" and "Focus on why users would want to interact" (marketing-adjacent). I keep the plain voice and use only the verb-first wording (see §1).

## 1. What TGDP recommends for a README

### What I read

- README template, raw text (6.3 KB of rendered markdown): https://gitlab.com/tgdp/templates/-/raw/main/readme/template_readme.md
- README guide: https://www.thegooddocsproject.dev/template/readme
- Quickstart guide: https://www.thegooddocsproject.dev/template/quickstart
- Troubleshooting guide: https://www.thegooddocsproject.dev/template/troubleshooting
- Reference guide: https://www.thegooddocsproject.dev/template/reference
- How-to guide (first ~2.8k of 12.4k chars only): https://www.thegooddocsproject.dev/template/how-to
- Tutorial guide (first ~2.6k of 10.9k chars only): https://www.thegooddocsproject.dev/template/tutorial
- Concept guide (first ~2.6k of 18.7k chars only): https://www.thegooddocsproject.dev/template/concept
- Release notes guide (first ~2.8k of 17k chars only; includes the release-notes vs changelog table): https://www.thegooddocsproject.dev/template/release-notes
- Installation guide (first ~2.8k of 10k chars only): https://www.thegooddocsproject.dev/template/installation-guide
- Style guide (first ~2.8k of 9.3k chars): https://www.thegooddocsproject.dev/template/style-guide
- Contributing guide (first ~2.8k of 11.8k chars): https://www.thegooddocsproject.dev/template/contributing-guide
- General writing tips (local clone of the repo): https://github.com/thegooddocsproject/templates (default branch `dev`), `writing-tips.md`, `templates-list.md`, and directory listings of quickstarts/, how-to/, reference/, tutorial/, explanation/, style-guide/, base/.

### What I could not read or verify

- **There is no `readme/` directory on the GitHub `dev` branch.** The brief's path `/readme/` 404s there (`https://raw.githubusercontent.com/thegooddocsproject/templates/main/readme/...` also 404s). The canonical repo is GitLab (`gitlab.com/tgdp/templates`). The GitHub copy is an older mirror whose `templates-list.md` still lists README, Installation guide, Release notes, and Contributing guide as "to create".
- **No `gh api` or shell:** this session had no shell tool, so I fetched through HTTP only. I did not run `./bin/shellshot --help` or the subcommand helps, so the CLI facts below come from README/docs and the goreleaser/Docker files, not the live binary.
- **No dedicated "CLI/command reference" template.** TGDP's Reference guide has an optional "Commands" section and says it covers "CLI commands and arguments". There is also an API reference template, which I did not read.
- **Chronologue** is a fictional telescope project TGDP uses for template examples. A web search says its GitHub repo was archived on 2022-09-24 (search-engine summary; `github.com/thegooddocsproject/chronologue` not fetched). I did not read any of its example READMEs, so I cannot quote what an example looks like.
- **Diataxis:** I did not find TGDP prose naming Diataxis. The how-to and tutorial guides use its vocabulary ("learning-oriented" vs "task-oriented"). A web-search summary claims the four-way split; see "Contradictions". Treat the Diataxis alignment as *interpretation*.
- Remaining halves of the how-to, tutorial, concept, release-notes, installation, style and contributing guides were not read. The Troubleshooting *template file* and Release-notes *template file* were not read, only their guides.

### The README template (direct evidence)

Section order in the template (README template, raw):

1. `{Project} logo and badges` (optional)
2. `{Project Name}` (URL and owner underneath, "if applicable")
3. `Table of contents` (optional)
4. `Project description`
5. `Who this project is for`
6. `Project dependencies`
7. `Instructions for using {Project}`: Install, Configure, Run, Troubleshoot (table of Issue/Solution plus FAQ/runbook links)
8. `Contributing guidelines`
9. `Additional documentation`
10. `How to get help`
11. `Terms of use`: "licensed under {link to license file}"

Required vs optional: the template marks only *logo and badges* and *Table of contents* as "optional". The guide, however, says "you can **reorder and remove sections or content** that do not apply". So nothing is mandatory; the guide's own text makes every section removable. (Interpretation.)

Key guide wording:

- Purpose: "tells users what they need to know… if the project is relevant to the user, and how users can engage."
- Voice: "approachable, friendly voice"; "strong verbs… such as 'instruct,' 'discover,' or 'build'"; "Be succinct"; "Avoid the passive voice."
- Description is "the most critical part": explain what it does and why to care; "Avoid describing any languages, technologies, or tools… until after" the strong description; state limitations or "when a user would not want to use the project".
- Description formula: "With {Project} you can {verb} {noun}…", "{Project} helps you {verb} {noun}…", "Unlike {alternative}, …".
- "Any assets you direct users to should exist in the repo."
- Audience: "intended for {target user} who wants to {user objective}."
- Prerequisites: give "context on why the prerequisites… are required"; link installation instructions.
- Steps: "typically formatted in an ordered list. Start each instruction with a verb."
- Troubleshooting: "You can use a table".
- Contributing: link to a guide, or embed it. "a smaller project may include guidelines in the README if… summarized in a few sentences."
- TOC: "GitHub now automatically generates a table of contents" (so a hand-written one is redundant on GitHub).
- Badges: "add credibility". Maintainer rule is stricter; omit.
- The README guide itself links the Quickstart template for "Getting Started" content.

### Other guides that bear on a README and docs split (direct evidence)

- **Quickstart** (TGDP): "focuses on the **primary feature**… as quickly as possible." Parts: Scope, Install (optional), "Hello World" end-to-end sanity check, Next steps. Rules: "Avoid complicating the quickstart by including error scenarios or complex use cases"; "Ensure that the quickstart works and provides the advertised result"; "Provide examples of sample output"; "Include one action in a step"; headings start with a verb, not -ing; "Provide instructions to verify that the installation is successful". Target is 1-2 hours, "a preference for a shorter time".
- **Installation guide**: two kinds, "Standalone" or "**Integrated**: … inside an existing README document". Distinguishes install ("how to install") from how-to ("how to do something… after it's been installed").
- **How-to**: task-oriented; "Do not use a how-to to teach concepts." **Tutorial**: learning-oriented, "Ideally… 15 to 60 minutes".
- **Reference**: "concise, structured information that a user can quickly scan. It is important to limit procedural or instructional content." Wants tables, active voice, "mirrors your application's structure", "Avoid high-level usage instructions".
- **Troubleshooting guide**: put "Symptoms" as bullet questions linked to solutions; numbered steps; "KISS"; prefer text over screenshots (maintenance). Its preamble asks whether you have capacity to maintain it.
- **Release notes vs changelog**: release notes are customer-focused, plain language, one owner; changelogs are developer-focused, from commit messages, with links to issues/commits. (The guide says changelogs are "typically… derived from commit messages".)
- **Contributing guide**: "Open source projects name this file in all caps: CONTRIBUTING.md"; needed when the project "relies on outside collaboration".
- **Style guide**: a stub is fine; "small amount of work up front can save… later" (record term decisions).
- **writing-tips.md**: "Avoid colloquialisms and jargon"; spell out acronyms on first use; "Avoid adding your own opinions"; steps are single sentences; title and filename should match; "Add any links you mention… into the 'See also' section".

## 2. Gap analysis of our README

Our README is 170 lines: logo, tagline, showcase, What it does, Install, Use, Script, Limits, Troubleshooting, licence line.

| Element | TGDP says | We have | Verdict | Why |
|---|---|---|---|---|
| Logo, name | Logo optional; name as H1 | Logo `<picture>` with alt "shellshot", dark/light variants; no Markdown H1 | Keep; **improve** | The logo carries the name but there is no `#` heading. Screen readers get alt text only; GitHub's outline/anchor navigation has no H1. Add a visually hidden-free option: keep logo, and make the logo `<h1 align="center">` wrapper (GitHub allows `<h1>` around the image), or accept the gap. Low priority. |
| Badges | Optional, "credibility" | None | **Deliberately omit** | Maintainer rule. |
| Table of contents | Optional, GitHub auto-generates | None | Keep omitted | At ~160 lines GitHub's outline suffices. |
| Project URL / owner | "if applicable" | None | Omit | Repo URL is the page itself; maintainer rule. |
| One-sentence description | Verb-led "With X you can…"; most critical part | Tagline: "Record terminal sessions. Replay them. Export them as animated PNGs, GIFs, WebPs or SVGs." Fragments, imperative, no subject; good verbs | **Improve** | It lists verbs, but says nothing about *what shellshot is* or *what it is for* (docs, READMEs, bug reports, CI artifacts). One line replaces "What it does" first sentence. |
| Why care / value | "why a user should care" | "What it does" explains mechanism (PTY, libghostty, event log) | **Improve** | Mechanism comes before the benefit; TGDP says defer tech details. Lead with the result (a small `.shot` file renders to an image without screen-recording); move libghostty to docs/ARCHITECTURE.md or one subordinate sentence. |
| Screenshot/demo | "Include screenshots" if assets in repo | Showcase PNG at top with descriptive alt | **Keep** | Matches; alt text is already informative (long but useful). |
| Who it is for | Explicit audience sentence | Not stated | **Add (one line)** | Fold into the description, no separate heading. Example in §3. |
| Prerequisites | Listed with reasons | Inside Install prose: Go 1.27.3+, cgo, Xcode CLT, git, pkg-config, zig 0.17; "Only macOS on Apple silicon is tested" | **Improve** | Only needed for source builds once binaries exist. Binary path needs one line: OS/arch. Move build prereqs to docs/development.md (which already has them) with a pointer. |
| Install | Verb-first ordered steps; verify success | Source build only: clone, `make deps` (minutes), `make build`, copy binary, `shellshot version` | **Improve (biggest gap)** | Counted steps: install zig + Go (2 prerequisites), 3 commands to build (clone, `cd`, deps, build = 4), then `mkdir/cp`, then `version` = about 7 commands and several minutes. Release archive: download, untar, move, `version` = 3-4. Container: 1 command (pull on first use). See §3. |
| Verify install | "instructions to verify" | `shellshot version` | Keep | Add the expected output line once confirmed. |
| Configure | Section in template | None; no config file | **Deliberately omit** | Nothing to configure; heading would be filler. |
| Quickstart / first result | Hello-world, one action per step, sample output | `record -c 'echo hello'` with output shown, then `export` with output, spread through ~70 lines of prose | **Improve** | Time-to-first-success (source build): about 7 install commands + 2 (record, export) + opening the PNG. Prose between them (exit status rules, DEC 2026, idle-limit, crop) violates "avoid… error scenarios" in a quickstart. Pull a 3-command, copy-paste block with expected output; push rules to reference. |
| Expected output | "Provide examples of sample output" | Shown for `record`, `export`, `info`, script | **Keep** | Strength of the current README. Mark the `console` blocks consistently (`$` lines vs output). |
| Use (record/replay/export) | Run section, ordered steps | Long paragraphs mixing how-to and reference | **Improve** | Move: exit codes, stdin pipe/Ctrl-D, replay crop/flicker, PNG viewer support, `--poster`, cell size to docs/. Keep one example each for record, replay, export. |
| Script | (no TGDP equivalent; a task) | Good: full script, command, output, `--list` and exit codes in one short section | **Keep; trim** | User asked for "how to script"; stays. Trim the second half (precedence, exit 3, "Reserved, not built") to a link. Reserved features belong in docs/scripting.md (already in its status line). |
| Limits | "specify… project limitations or when a user would not want to use" | 6 bullets, several reference-level (GIF colours, Apple emoji licence) | **Keep; shorten** | Fits TGDP and helps users avoid pitfalls. Keep: terminal size fixed, emoji/CJK coverage, platform support, Apple artwork licence. Move GIF/WebP/blink detail to docs/export.md (already there). |
| Troubleshooting | Table Issue/Solution; link FAQ | 7 bullets; 3 are build errors, 4 are runtime | **Improve** | Split by audience: build errors duplicate docs/development.md's Troubleshooting; once binaries exist most users never see them. Keep runtime bullets (blank glyph, `wait` passes at once, no colour, flicker) in README as a table or leave bullets (table optional). Verdict on a table: bullets are fine; a table only helps if there are 6+ rows. |
| Contributing | Link, or embed if short | Nothing in README; no CONTRIBUTING.md | **Add one-line pointer** + file | See §2.2. |
| Additional docs | Links with brief descriptions | Scattered inline links through Use/Script | **Improve** | A 4-line "Documentation" list ends scatter and satisfies TGDP's "See also" advice. |
| How to get help | Support channels | None | **Add one line** | "Report bugs and ask questions: GitHub Issues". Do not create a Support section. |
| Licence | "Terms of use: licensed under…" | Last line, MIT + NOTICE link | **Keep** | Matches; also the one place Apple-artwork liability is linked. |
| Project structure | Not in the TGDP template | Absent | **Deliberately omit** | Maintainer rule; TGDP agrees (not a listed section). |

### 2.1 Concrete checks

- **First paragraph:** the tagline is three imperatives and a list of formats. It tells *what you can do* but not *what it is* or *who it is for*. Fails TGDP's "With X you can…" test. Two clauses fix it (§3).
- **Time to first success from clone (current):** prerequisites (Go 1.27.3+, zig 0.17, Xcode CLT, pkg-config, git) → `git clone` → `cd` → `make deps` ("several minutes") → `make build` → `mkdir -p ~/.local/bin && cp …` → `shellshot version` → `shellshot record -o demo.shot -c 'echo hello'` → `shellshot export demo.shot -o demo.png`. That is 8 commands after installing toolchains; 10 if you count the two toolchain installs. The recording and export steps themselves are 2 commands and fast.
- **With release archive (proposed):** `curl`/download, `tar`, move to PATH, `shellshot version`, then the same 2 commands: 6 total.
- **With container (proposed):** `docker run --rm -v "$PWD:/work" ghcr.io/mikelorant/shellshot record -o demo.shot -c 'echo hello'` then the same with `export demo.shot`: 2 commands. Facts from `Dockerfile.release`: entrypoint `shellshot`, `WORKDIR /work`, user uid 1000, `SHELL=/bin/bash`, `TERM=xterm-256color`. **Unverified:** that the bind mount is writable by uid 1000 on Linux hosts (Docker Desktop on macOS usually is); that the published image is public (docs/development.md says new GHCR packages start private, public flip is a manual step); whether a `docker-compose.yml` exists (not in repo root; docs/development.md did not mention one in what I read).
- **Copy-pasteable quickstart with expected output:** present but fragmented. The script example is good and self-contained.
- **Prerequisites explicit:** yes for build; absent for running a binary (OS, arch). Also README says "Only macOS on Apple silicon is tested", while CI tests `ubuntu-latest` and releases ship Linux binaries and a Linux image (docs/development.md, goreleaser). **Contradiction to resolve before rewriting the platform line.**
- **Linking to deeper docs:** links go to `docs/export.md#anchors`, which is good. They are inline, not collected.
- **Troubleshooting placement:** at the end, after Limits; that is where TGDP's template puts it (inside "Instructions", last step). Fine. But build-error entries duplicate `docs/development.md`.
- **Accessibility/scannability:**
  - Headings: H2 only below an `<img>` title; fine. Add H1 for the project name (see table).
  - Alt text: logo alt "shellshot" (ok); showcase alt is descriptive (ok).
  - Code fences: mostly tagged `sh`/`console`; the script example fence at README lines ~118-131 is **untagged** (a `.script` block); either tag as `text` or leave. Docs/export.md uses tables well.
  - Line length: README wraps near 100; one paragraph in Use (`record` ... "never replaces an existing file. The terminal size…") runs unwrapped for ~160 characters, an inconsistency.
  - Link text: all descriptive ("docs/export.md#poster-png" as a label). The `--poster` link is an anchor-only label; fine.
  - Dense paragraphs: "Use" has paragraphs of 6-8 sentences; TGDP/KISS says short steps.
- **Terminology consistency (README text):** (a) "animated PNG(s)" in the tagline, `png` as format name, and "PNG" in prose are consistent enough, but docs/export.md says "Animated PNG" in the table and README says "apng" nowhere, so no actual `apng` inconsistency found in the README; docs/ARCHITECTURE.md uses `export/apng` as a package name (fine). (b) "recording" is used for the `.shot` file and the act; "session" is used for the live terminal (README "Record terminal sessions") and `session` is a code package in ARCHITECTURE. Acceptable, but pick: *recording* = file, *session* = live terminal. (c) "export" is both verb and subcommand; fine. (d) docs/ARCHITECTURE.md says shellshot "replay" of `.shot`; README same. Not checked beyond heads of docs.
- **Voice:** TGDP asks for strong verbs and active voice. The README is mostly active/imperative ("Save this as", "Pass `--color always`"). Passive/stative drift appears in Limits ("is dropped", "are not drawn"). Fine for limits.

### 2.2 Support, contributing, licence, security, changelog: one-liner or file?

| Item | Evidence | Verdict for v0.1.0 |
|---|---|---|
| Licence | TGDP "Terms of use" | Keep the existing one-line link. |
| Contributing | TGDP: link a guide, or embed if "summarized in a few sentences"; contributing guide says needed if "relies on outside collaboration" | **Add `CONTRIBUTING.md` (short: build, `make vet test build`, Conventional Commits, one PR per task; all of this already lives in docs/development.md "Worktrees").** README gets one line. If you do not want outside PRs, say so in that file instead. |
| Support / "How to get help" | TGDP lists channels | One line: "Issues: <url>". No section. |
| Security | Not in the TGDP README template | **Add `SECURITY.md` (3-5 lines: how to report privately).** GitHub surfaces it in the Security tab. No README text. Rationale (inference): a tool that runs shells and stores terminal output can capture secrets; a reporting address matters. |
| Changelog | TGDP separates release notes (plain language, one owner) from changelogs (from commits) | **Do not add CHANGELOG.md.** GoReleaser already generates grouped notes for each draft GitHub Release (`feat`, `fix`, Others). Treat the GitHub Release page as the release notes; hand-edit for v0.1.0 per docs/development.md. Link from README? One line "Releases" in the install section is enough. |
| Code of Conduct, governance, issue/PR templates | TGDP community pack | **Filler for v0.1.0.** |

## 3. Proposed README outline (suggestion, not applied)

Target 150-165 lines. Logo, tagline, showcase stay at the top.

| # | Heading | Purpose | Est. lines |
|---|---|---|---|
| – | (logo, tagline, showcase) | Keep as is, tagline reworded (below) | 7 |
| 1 | `## What it does` | 3-5 lines: what it is, who it is for, the one benefit; the formats; mention scripts; mechanism only in a link | 8 |
| 2 | `## Install` | Ordered by ease: container, release archive, build from source (link) | 22 |
| 3 | `## Quickstart` | Record, export, view: copy-paste with expected output | 24 |
| 4 | `## Use` | Replay, info, flags worth knowing, batch export; each with one example | 38 |
| 5 | `## Script` | Current example; trim second half | 28 |
| 6 | `## Limits` | 4-5 bullets that change decisions (size fixed, fonts, platform, Apple emoji licence) | 10 |
| 7 | `## Troubleshooting` | Runtime problems only (4 bullets); build problems link to docs/development.md | 10 |
| 8 | `## Documentation` | 4-5 links: export, scripting, format, examples (contrib), development | 8 |
| 9 | (closing lines) | Licence, issues/support, contributing, one line each | 4 |

Total about 159. No new top-level sections beyond Quickstart and Documentation.

### Suggested opening (draft)

```md
<p align="center"> … logo … </p>

<p align="center">Record terminal sessions and turn them into animated PNG, GIF, WebP or SVG images.</p>

<p align="center"> … showcase … </p>

## What it does

`shellshot` records a shell session or one command and exports it as an animated image you can
put in a README, issue or chat. It is for developers who document command-line tools and want
images that come from a script, not from screen-recording software. A recording is a small
text-based `.shot` file that you can replay in the terminal, re-export at any size or theme, and
produce again in CI.
```

Notes: this is one "With X you can…" sentence plus an audience sentence plus a value sentence (3 lines). Check the claims before using: "re-export at any size or theme" is stated in docs/ARCHITECTURE.md ("themes, fonts and speed are export-time choices"); "in CI" follows from the README's exit-status paragraph. `.shot` is *binary* per docs/format.md ("chunked binary event log"), so do **not** write "text-based".

### Suggested install block (draft, order of ease)

```md
## Install

Container (needs Docker; Linux amd64/arm64):

    docker run --rm -v "$PWD:/work" ghcr.io/mikelorant/shellshot version

Release archive (macOS or Linux, amd64 or arm64), from the
[releases page](https://github.com/mikelorant/shellshot/releases):

    tar -xzf shellshot_<version>_<os>_<arch>.tar.gz
    mv shellshot ~/.local/bin/ && shellshot version

From source: see [docs/development.md](docs/development.md#build).
```

Archive name format `shellshot_{{ .Version }}_{{ .Os }}_{{ .Arch }}.tar.gz` is from `.goreleaser.yaml`; archives also contain README.md, LICENSE and NOTICE; checksums are in `checksum.txt` (sha256). macOS Gatekeeper may quarantine a downloaded unsigned binary (**unverified**; the goreleaser config does not show signing or notarisation). If quarantined, a one-line `xattr -d com.apple.quarantine` hint belongs in Troubleshooting. Add that only after testing.

Pitfalls to avoid (researcher inference): a README inside the archive has relative `docs/...` links that break when read from the unpacked archive; the archive ships README.md but not docs/. Either link to absolute URLs for docs, or drop README.md from the archive.

### Suggested quickstart block (draft)

```md
## Quickstart

Record a command, then export it as an animated PNG:

    shellshot record -o demo.shot -c 'echo hello'
    shellshot export demo.shot -o demo.png

    recorded 2 events (0.3 KB) to demo.shot
    wrote 1 frames (9.1 KB) to demo.png

Open `demo.png` in a browser. macOS Preview shows only the last frame.
```

Output lines come from the current README (`recorded 2 events (0.3 KB) to demo.shot`; `wrote 1 frames (9.1 KB) to demo.png` was shown with `--scale 2 --theme light`, so the byte count will differ). Re-run and paste real output.

### What to move out of the README

| Currently in README | Move to |
|---|---|
| `--idle-limit`, DEC 2026, crop/`--no-crop`, Ctrl-C exit 130, summary and `-q` | new `docs/replay.md`, or a short "Replay" section in `docs/export.md`? Better: a new file because export.md is already ~350 lines. Reference style. |
| stdin pipe sends Ctrl-D; non-zero exit status propagation | `docs/record.md` (how-to/reference), or the "Running a script" part of docs/scripting.md for script-side; interactive part needs a home. |
| `--poster`, viewer support, cell size, GIF colours, WebP unverified | already in docs/export.md; delete duplicates from README. |
| Build-error troubleshooting (3 bullets), toolchain prerequisites | docs/development.md (already has them). |
| "Reserved, not built" line | docs/scripting.md status line (already has it). |

## 4. docs/ organisation and recommendations beyond the README

### Current docs vs TGDP/Diataxis-style types (interpretation)

| File | Type it is | Notes |
|---|---|---|
| README.md | Mixed: overview, quickstart, how-to, reference | Needs splitting as above. |
| docs/export.md | **Reference** (titled "Export reference"; format table, flags) plus explanation (GIF palette maths, sizes, "measured error") | Mostly consistent. Explanation material (palette algorithm, measurement numbers) could move to docs/research/ or stay under "why". |
| docs/scripting.md | **Reference** with how-to elements ("Running a script") | Title "Script format". Fine. Quick tutorial-style walkthrough is absent. |
| docs/format.md | **Reference** (binary spec) | Good as a reference; the "design discussion" link to research/ is explanation. |
| docs/ARCHITECTURE.md | **Explanation** (data flow, "how it is put together") | For contributors; correctly not linked from README. |
| docs/development.md | Mixed how-to, reference (make targets table), release runbook, **Troubleshooting** | Misfiled: "Continuous integration and releases" and "Bumping the libghostty pin" are maintainer runbooks; consider `docs/releasing.md`. Not urgent. |
| docs/research/* | Design history (explanation) | Correctly flagged as historic in research/README.md. |
| contrib/README.md, contrib/examples/README.md | Reference + how-to for demo scripts | Fine. contrib/README.md is 5 lines. |
| NOTICE | Legal | Fine. |

Misfiled for a user: runtime troubleshooting appears in both README and development.md (two replay bullets duplicated verbatim), and replay behaviour has **no reference doc** at all (it lives only in README prose).

### Which TGDP templates are worth adopting

| Template | Use for | Verdict |
|---|---|---|
| Quickstart | README `Quickstart` section | **Adopt the structure** (Scope, Hello World, Next steps), not a separate file. |
| Reference (Commands section) | `docs/cli.md` or generated from `--help` | **Adopt, generated.** Hand-maintained duplication of flags drifts. |
| Installation guide ("Integrated") | README Install section | Adopt as the integrated variant. |
| Troubleshooting (symptom → steps) | README runtime bullets | Keep the existing symptom-first bullet format; matches the template's "describe each symptom". Separate file not needed yet. |
| Release notes | GitHub Release body | Adopt the idea (plain language, one owner) by editing the draft; no template file. |
| Contributing guide | `CONTRIBUTING.md` | Adopt, short. |
| Tutorial | Optional later | Only if users ask; one scripted-demo tutorial could live in `contrib/examples`. |
| Concept (explanation) | "How a recording works": event log → frames → export | Optional; docs/ARCHITECTURE.md covers it for contributors. |

### Ranked task list (small, atomic; hand to workers later)

1. **README: rewrite description (3 lines + tagline).** Accept: states what it is, who it is for, one benefit; no mechanism before benefit; the claims match docs/format.md and docs/ARCHITECTURE.md.
2. **README: replace Install with container / archive / source order.** Accept: each path was run on a clean machine by a human; `shellshot version` output shown; platform support line matches CI and release matrix; checks for GHCR visibility and archive filenames done. (Blocked until v0.1.0 assets exist.)
3. **README: add Quickstart block with real output.** Accept: run from a clean directory; two commands; output pasted from an actual run; PNG opens.
4. **Create `docs/replay.md`** (or add a section) holding the replay and record behaviour removed from README. Accept: every flag in `shellshot replay --help` and `record --help` appears once; no behaviour lost; README links it.
5. **README: trim Use and Script to examples + links.** Accept: README ≤165 lines; each moved fact exists in exactly one docs/ file; no broken anchors (link checker or manual).
6. **README: add Documentation list (5 links) and closing lines (Issues, Contributing, Licence).** Accept: every link resolves; relative links work on GitHub.
7. **Create `CONTRIBUTING.md`** (≤30 lines, built from docs/development.md). Accept: `make vet test build` command correct; states PR expectations.
8. **Create `SECURITY.md`** (≤10 lines). Accept: gives a private contact route (GitHub private vulnerability reporting or email).
9. **Deduplicate Troubleshooting:** README keeps runtime items; development.md keeps build items. Accept: no entry appears in both.
10. **Generate `docs/cli.md` from `--help`** (script or `make docs`), checked in CI like `check-showcase`. Accept: regenerating yields no diff; covers every subcommand.
11. **Fix README formatting:** tag the `.script` code fence, wrap the long paragraph, add H1 if desired. Accept: markdown lint passes (e.g. markdownlint, not verified to be installed).
12. **Terminology pass:** "recording" = file, "session" = live terminal; "animated PNG" on first use, "PNG" after. Accept: grep of README and docs shows no mixed use.
13. **Optional:** move CI/release runbooks from development.md to `docs/releasing.md`.

## 5. Rejected as bloat

- Badges, logo row of links, project URL and owners: maintainer rule; TGDP marks as optional.
- Table of contents: GitHub auto-generates.
- Configure section: no config file.
- Project structure section: maintainer rule; not in TGDP template.
- Separate Quickstart/Tutorial/How-to files for each task: a one-binary CLI does not need a docs site. Link to existing reference docs.
- Troubleshooting as an HTML table: the template's `<table>` is verbose HTML; bullets are easier to maintain.
- CHANGELOG.md hand-written: duplicates GoReleaser output.
- Glossary, FAQ, user personas, Code of Conduct, governance: TGDP community pack items with no value at v0.1.0.
- Docs site, Sphinx or mkdocs: not justified by the evidence.
- "Friendly, approachable voice" and "why users would want": risk of marketing tone; keep plain imperative voice.

## 6. Risks and open questions

1. Platform claim: README says only macOS Apple silicon is tested; CI and release cover Linux and macOS (docs/development.md, goreleaser). What can we promise on the README?
2. GHCR package visibility: public flip is manual (development.md). Do not document `docker run` until it works anonymously.
3. Container usage: bind-mount permissions for uid 1000, `-it` for interactive `record`, TTY size defaults (80x24 when stdin is not a terminal).
4. Apple emoji artwork: the release archives and image embed it. README currently warns; NOTICE must stay linked. Check whether pre-built distribution changes the licence risk (maintainer decision; not a legal opinion).
5. Unsigned binary on macOS: Gatekeeper behaviour not tested.
6. Archive README links break (relative `docs/`).
7. I did not run `--help`, so I could not check the README against flag names or defaults. `record`'s `--idle-limit`, `--poster`, `--watch` etc. come from existing docs only.
8. Whether `docker-compose` exists: not found at repo root; the brief says it does. Not mentioned in README.
9. Two sources about TGDP and Diataxis: see Contradictions.
10. Maintainer decision: README H1. The logo image has no heading semantics.
11. `docs/export.md` lines 41+, `scripting.md` lines 41+, `format.md` lines 31+, `ARCHITECTURE.md` lines 31+ and `development.md` lines 41-169 were not read; verdicts for those files are based on titles and heads, so mark as medium confidence.

## Contradictions

- **README template says Install/Configure/Run/Troubleshoot are all sub-steps of "Instructions for using"**, while the Quickstart guide says quickstarts should avoid error scenarios and the Troubleshooting guide treats troubleshooting as its own document. Resolution (my interpretation): keep a short Troubleshooting section in the README and keep it out of the Quickstart.
- **Diataxis:** a search-engine answer says TGDP provides "Diátaxis-style templates" (reference: a Google opendocs page, `google/opendocs`, not fetched). TGDP pages themselves use "learning-oriented" and "task-oriented" but name "Concept" where Diataxis says "Explanation", and the GitHub repo still lists a separate `explanation/` dir. Not resolved. Treat the alignment as loose.
- **TGDP GitHub vs GitLab:** the brief and a naive path point to `github.com/.../templates/readme/`, which does not exist; the content is on GitLab.
- **Our README vs release matrix:** platform support (see §6.1).

## Missing evidence

- No fetched example README from TGDP (the Chronologue examples were not read).
- No TGDP "CLI reference" template; only the Reference guide's Commands section.
- No usability data for time-to-first-success; step counts are my arithmetic from README text.
- Quoted snippets above were taken from fetched pages (each under 25 words); I did not independently re-verify each quotation against a second source (source_check not used).

## Sources

- Kept: README template raw — https://gitlab.com/tgdp/templates/-/raw/main/readme/template_readme.md — canonical section order and placeholders
- Kept: README guide — https://www.thegooddocsproject.dev/template/readme — wording and optional-section guidance
- Kept: Quickstart guide — https://www.thegooddocsproject.dev/template/quickstart — hello-world and output rules
- Kept: Troubleshooting, Reference, How-to, Tutorial, Concept, Release notes, Installation, Style, Contributing guides (URLs under https://www.thegooddocsproject.dev/template/…, each partially read as listed in §1)
- Kept: https://github.com/thegooddocsproject/templates (dev branch, local clone) — `writing-tips.md`, `templates-list.md`, directory listings
- Kept (local): README.md, docs/export.md (head), docs/scripting.md (head), docs/format.md (head), docs/ARCHITECTURE.md (head), docs/development.md (1-40, 170-319), contrib/README.md, contrib/examples/README.md, Dockerfile.release, .goreleaser.yaml. Absent: CONTRIBUTING.md, SECURITY.md, CHANGELOG.md, docker-compose.yml (at root).
- Deprioritized: web-search summaries (Chronologue archived, Diataxis-style claim) — secondary, unverified.
- Not read: https://www.thegooddocsproject.dev/chronologue (404), `gh api` (no shell).

## Next steps

1. Run `./bin/shellshot --help` and each subcommand's help, then check flag names and defaults in the README rewrite.
2. Test the container and archive paths on a clean machine after the first release.
3. Decide on the platform claim, GHCR visibility, and whether to ship README.md in archives.

```acceptance-report
{
  "criteriaSatisfied": [
    {
      "id": "criterion-1",
      "status": "satisfied",
      "evidence": "Brief written to docs/research/readme-improvements.md with summary first, gap table, outline, task list, rejections, risks; residual risks and gaps listed."
    }
  ],
  "changedFiles": ["docs/research/readme-improvements.md"],
  "testsAddedOrUpdated": [],
  "commandsRun": [
    {"command": "shell commands (gh api, bin/shellshot --help)", "result": "not-run", "summary": "No shell tool available; used HTTP fetch and file reads instead"}
  ],
  "validationOutput": ["Web pages fetched via fetch_content; local files read via read; source_check not used"],
  "residualRisks": [
    "Several TGDP guides read only in part; long local docs read only at their heads",
    "CLI --help not run, so flag claims unverified",
    "No read-date stamp available; add when filing",
    "Container/archive install paths untested; GHCR visibility unverified"
  ],
  "noStagedFiles": true,
  "diffSummary": "Added one new research brief; no other files changed.",
  "reviewFindings": ["no blockers"],
  "manualNotes": "README template exists only on GitLab (gitlab.com/tgdp/templates); GitHub mirror lacks readme/. Maintainer rules took precedence over TGDP optional sections."
}
```
