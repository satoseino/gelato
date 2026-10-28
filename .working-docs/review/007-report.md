---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/9
changes_requested: [M1, m2, m3]
findings_dismissed: [m1]
merged:
notes:
---
# PR Review: Feature: publish gelato as a pre-commit hook, GoReleaser binaries and Homebrew formula

**PR:** https://github.com/satoseino/gelato/pull/9
**Spec:** .working-docs/spec/007-distribution-and-migration.md
**Reviewed:** 2026-10-27

## Summary

The PR matches the spec closely: `LICENSE`, the third-party license generator, `.goreleaser.yaml` with a `brews` formula, the tag-triggered release workflow, the `language: golang` hook and a README that covers every point in the spec. I ran `HOMEBREW_TAP_TOKEN=unused goreleaser release --snapshot --clean` locally and it met the snapshot acceptance criteria. The only substantive problem is in the release toolchain: `setup-go` reads `go 1.26.0` from `go.mod` as an exact version, so every release binary would be built with Go 1.26.0 and without its later stdlib security fixes. Everything else is minor.

## Spec Compliance

| Spec item | Status | Notes |
|---|---|---|
| 1. `LICENSE` (MIT, Ke Xue) | Met | |
| 2. `scripts/third-party-licenses.sh` | Met | Mode `100755`. `LICENSE*`/`COPYING*`/`NOTICE*` are globbed, there is a `GOROOT/../LICENSE` fallback, and it fails when a file is missing. A local run produced sections for kong, go-toml/v2, x/crypto, x/sys, x/term and the Go standard library. |
| 2. `.gitignore` entries | Met | See [m2]. |
| 3. `.goreleaser.yaml` builds/archives/checksum | Met | Snapshot: 6 archives plus `checksums.txt`. Each contains the binary, `LICENSE`, `README.md` and `THIRD_PARTY_LICENSES`. |
| 3. `brews` entry | Met | `dist/homebrew/Formula/gelato.rb` has darwin and linux URLs for both arches, `prefix.install "THIRD_PARTY_LICENSES"` and the `--version` test. The deprecation warning appears as the spec expects. |
| 4. `release.yaml` | Met (spec-literal) | Matches the spec exactly, but see [M1] on what `go-version-file: go.mod` resolves to. |
| 5. `.pre-commit-hooks.yaml` | Met | Byte-for-byte as specified. `cmd/gelato` is the only `main` package, so `go install ./...` installs only `gelato`. |
| 6. `README.md` | Met | Every listed section is present. The flags (`-V`), the exit codes, the `repo-path` semantics and the example error text `tartufo.toml: exclude-entropy-patterns[0].scope: unknown key` match `cmd/gelato/main.go`, `command/command.go` and `cfg/config.go`. |
| `CLAUDE.md` release section | Met | |
| AC: snapshot archives + checksums | Met | Verified locally. |
| AC: formula contents | Met | Verified locally. |
| AC: `--version` + `CGO_ENABLED=0` | Met | Verified locally: `0.0.0-SNAPSHOT-49e2854`, `build CGO_ENABLED=0`. |
| AC: `THIRD_PARTY_LICENSES` sections | Met | Verified locally. |
| AC: pre-commit hook blocks a secret | Met (per PR description) | Not re-run in this review. |
| AC: `make all` | Met (per PR description) | Not re-run in this review. |

## Findings

### Critical

_None._

### Major

**[M1] Release binaries are built with the exact Go version 1.26.0, without later patch releases** — `release.yaml` uses `actions/setup-go@v5` with `go-version-file: go.mod`. `go.mod` has `go 1.26.0` and no `toolchain` directive. setup-go uses the `toolchain` directive when there is one, otherwise the `go` directive, and "if a patch version is specified, that specific patch version will be used" (actions/setup-go `docs/advanced-usage.md`). `GOTOOLCHAIN=auto` won't upgrade either, because 1.26.0 already satisfies the `go` line. So every tagged release statically links the Go 1.26.0 standard library and misses every 1.26.x security fix (`net/http`, `crypto/*`, `os/exec`, and so on). For a security tool shipped as static binaries, that will show up in `govulncheck` and SCA scans of the published artifacts. The test-and-lint workflow has the same pin, so CI tests the same stale toolchain and nothing catches the problem. Fix options:
- Add `toolchain go1.26.<latest>` to `go.mod`. setup-go then installs that exact patch release, and Go uses it locally as well, so CI and releases match. Each patch upgrade then needs a bump, which Dependabot/Renovate can automate.
- Or set `go-version: '1.26.x'` (or `stable`) in `release.yaml` and keep `go-version-file` only in the test workflow. This is simpler but lets the release and test toolchains drift apart.

  Spec 4 asked for `go-version-file: go.mod`, so the spec text may need a matching note.

  **Reviewer's chosen fix:** set `go-version: '1.26.x'` in `release.yaml`.

  *Status:* fixed in 699fcb4. `release.yaml` now uses `go-version: "1.26.x"`; the test-and-lint workflow keeps `go-version-file`.

### Minor

**[m1] The PR description has a garbled "Release" bullet** — the bullet reads "…publishes a Homebrew formula to `satoseino/homebrew-tap`. It uses `brews`, not casks, because casks quarantine the unsigned macOS binaries. pushes to the tap with `HOMEBREW_TAP_TOKEN`.elease.yaml` runs on `v*` tags. It pins GoReleaser to `~> v2`, the last major version with `brews`, and ". The sentence about `.github/workflows/release.yaml` has been spliced into the middle of another sentence. Earlier PRs were squash-merged (`… (#8)`), so this text would probably end up in the squash commit message on `main`.

**[m2] `.gitignore` gains a second consecutive blank line** — the hunk adds an empty line after the existing blank line that follows `/tartufo/`, so the file now has two blank lines in a row before the new comment. Fix: drop the added empty line.

  *Status:* fixed in ce6dabd.

**[m3] `gelato --version` prints `dev` when gelato is installed through the pre-commit hook** — pre-commit builds the hook with plain `go install ./...` and no `-ldflags`, so `main.version` keeps its default `"dev"`. Users who install through the hook, which is the main migration path, can't see which `rev` they are running from `--version`. Since Go 1.24, `go build`/`go install` in a git checkout stamps the main module version from VCS tags into `debug.ReadBuildInfo().Main.Version`, and pre-commit clones the hook repository with git. Fix option: when `version == "dev"`, fall back to `debug.ReadBuildInfo().Main.Version` if it isn't `(devel)`. The spec rules out Go changes beyond what release builds need, so this could also be a follow-up instead of a change in this PR.

  **Reviewer's chosen fix:** the `debug.ReadBuildInfo().Main.Version` fallback above.

  *Status:* fixed in f9ad448: `resolveVersion` in `cmd/gelato/main.go`, plus the table test `TestResolveVersion`. A real `pre-commit install-hooks` from a clone, with `rev: v9.9.9` and again with `rev: <sha>`, prints `v0.0.0-20261028030605-f9ad448604c5+dirty` in both cases. That names the commit but not the tag: pre-commit fetches the `rev` with `--depth=1` and no tags, and its `golangenv-*` directory inside the clone makes Go mark the build `+dirty`.

## Positive Observations

- The license script is robust. It uses `set -euo pipefail`, writes to a temp file and then does an atomic `mv` with a `trap` cleanup, uses `nullglob` for the per-module globs, and fails on a missing license, which covers kong's `COPYING`-only layout. It also handles Homebrew's `GOROOT` layout explicitly.
- The comments in `.goreleaser.yaml` and `release.yaml` explain the non-obvious decisions where they are made: `brews` over casks, the `~> v2` pin and the cross-repository PAT. The new `CLAUDE.md` section records them durably.
- The README's tartufo comparison is concrete and accurate against the code, and the migration section names the tartufo keys gelato rejects.
- `skip_upload: auto` keeps release candidates out of the tap, and `extra_install` puts the BSD-3 notices in the keg.

## Recommendation

**Request Changes** — [M1] means every published binary would ship an outdated Go standard library. Pinning a `toolchain` directive or using a floating patch version in the release workflow is a small change; the rest are nits.
