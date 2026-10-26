# Publish gelato (pre-commit hook and GoReleaser binaries) and validate it against tartufo

## Background

After specs 1–6, gelato works but can't be installed: there is no `LICENSE`, `README`, hook manifest or release config, and `cmd/gelato/main.go`'s `version` stays `"dev"`. It is meant to replace tartufo's hook (`tartufo/.pre-commit-hooks.yaml`: `entry: tartufo`, `language: python`, `args: [pre-commit]`, `pass_filenames: false`), whose `pygit2` dependency breaks on Python minor upgrades. Teams must be able to switch without editing `tartufo.toml`. Before anyone switches, a comparison against tartufo on a real repository (`jarvis-registry`) has to show the differences are only the intended ones.

---

## Changes

### 1. `LICENSE`

MIT License, `Copyright (c) 2026 Ke Xue`.

### 2. Third-party license notices — `scripts/third-party-licenses.sh`

Go binaries statically include their dependencies (kong and go-toml: MIT; `golang.org/x/crypto`, `x/term` and `x/sys`: BSD-3), and those licenses require their notices to ship with binaries. The script lists the modules linked into `./cmd/gelato` (`go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' ./cmd/gelato | sort -u`) and concatenates each module's `LICENSE*`/`NOTICE*` files, under a heading per module, into `THIRD_PARTY_LICENSES`. It fails if a module has no license file. The file is generated at release time (GoReleaser `before` hook), not committed; `/THIRD_PARTY_LICENSES` and GoReleaser's `/dist/` go into `.gitignore`.

### 3. `.goreleaser.yaml` (GoReleaser OSS, config `version: 2`)

- `before.hooks`: `go mod tidy`, `./scripts/third-party-licenses.sh`.
- `builds`: `main: ./cmd/gelato`, `binary: gelato`, `env: [CGO_ENABLED=0]`, `goos: [linux, darwin, windows]`, `goarch: [amd64, arm64]`, `ldflags: -s -w -X main.version={{ .Version }}`.
- `archives`: `tar.gz`, `zip` for Windows, including `LICENSE`, `README.md`, `THIRD_PARTY_LICENSES`.
- `checksum.name_template: checksums.txt`.
- No Homebrew or signing (see follow-up).

### 4. `.github/workflows/release.yaml`

On `push` of tags `v*`: `actions/checkout@v4` (`fetch-depth: 0`, needed for GoReleaser's changelog), `actions/setup-go@v5` with `go-version-file: go.mod`, then `goreleaser/goreleaser-action@v7` (`distribution: goreleaser`, `args: release --clean`) with `GITHUB_TOKEN`. Same action versions as `jarvis-registry-cli/.github/workflows/release.yaml`.

### 5. `.pre-commit-hooks.yaml`

```yaml
- id: gelato
  name: gelato
  description: Scan lines added in staged changes for secrets.
  entry: gelato
  args: [pre-commit]
  language: golang
  pass_filenames: false
```

pre-commit builds `language: golang` hooks with `go install ./...` from the hook repository, downloading a Go toolchain into its own environment when needed (visible in pre-commit issue #3512's traceback: `golangenv-default/.go/bin/go install ./...`). The build is cached per hook `rev`, not per Python version, which removes tartufo's breakage on Python upgrades. gelato has no cgo dependencies (`make build` enforces this with `CGO_ENABLED=0`), so pre-commit's default cgo setting doesn't matter.

### 6. `README.md`

- What gelato is, and that it reads tartufo's `[tool.tartufo]` config and produces the same signatures. It is an independent MIT implementation, not a fork; no tartufo code or data is included.
- Install as a pre-commit hook (`repo: https://github.com/satoseino/gelato`, `rev: vX.Y.Z`, `id: gelato`) and as a binary from GitHub Releases. Requirement: `git` ≥ 2.30 on `PATH`.
- Usage: `gelato pre-commit`, `gelato scan [REPO_PATH]`, `--config`, `--no-color`, `--version`; exit codes 0 / 1 / 2.
- Supported config keys (spec 1) with an example; unsupported tartufo keys are rejected with an error.
- **Differences from tartufo:**
  - only added lines are scanned;
  - its own default regex rules;
  - findings deduplicated within a file change and, in `scan`, across commits (reported at the oldest commit);
  - file-name findings have different signatures;
  - `pre-commit` detects staged renames, so a renamed file's unchanged lines aren't rescanned;
  - `regex`/`entropy` in the repository's own `tartufo.toml` take effect (tartufo honors them only from `--config`), and an unparsable config file is an error rather than ignored;
  - output shows only the matching added lines;
  - history covers branches, remote-tracking branches and `HEAD`, with every root and shallow-boundary commit diffed against the empty tree.
- **Migrating from tartufo:** replace the tartufo hook entry in `.pre-commit-config.yaml` with gelato's; keep `tartufo.toml` as is; expect possible new regex findings from the different rule set; list any keys gelato rejects.

### 7. Parity check against tartufo — `scripts/parity.py`

A standard-library-only Python script, run as `python3 -I scripts/parity.py --tartufo-json T.json --gelato-text G.txt`, that compares the issues each tool reports:

- Tartufo side: `found_issues` from `tartufo --output-format json scan-local-repo <repo>`, kept only when some line of the issue's `diff` that starts with `+` (and not `+++`) contains `matched_string`. This keeps only findings from added lines, which is all gelato scans.
- gelato side: the `Reason:`/`Signature:` pairs from `gelato --no-color scan <repo>`.
- Prints counts and lists, split by issue type: in both, tartufo-only, gelato-only.

Run it on `jarvis-registry` with its `tartufo.toml`. Record the commands, both tools' wall times (spec 6's acceptance criterion), the three buckets, and an explanation for every entropy-type difference in `.working-docs/session/parity-jarvis-registry.md`. Entropy differences are expected only from the documented divergences: diff hunk alignment, file-name tokens, and roots or shallow boundaries tartufo didn't diff. Regex differences are expected, because the rule sets differ, and are reviewed for false positives. The script and notes don't run in CI.

---

## What does NOT change

- No Go code changes beyond what release builds need; `main.version` already exists (spec 1).
- No CI workflow for tests and lint, no Homebrew tap, no code signing.
- No edits to `jarvis-registry` itself; its hook switch and the `\\.env\\.example$` pattern fix are done separately in that repository.

---

## Acceptance Criteria

- [ ] `goreleaser release --snapshot --clean` produces six archives (linux, darwin, windows × amd64, arm64) plus `checksums.txt`, each containing `gelato`/`gelato.exe`, `LICENSE`, `README.md` and `THIRD_PARTY_LICENSES`.
- [ ] The snapshot binary reports the snapshot version via `gelato --version`, and `go version -m` on it shows `CGO_ENABLED=0`.
- [ ] `THIRD_PARTY_LICENSES` has a section for every non-main module listed by `go list -deps` for `./cmd/gelato`.
- [ ] In a scratch repository, a `.pre-commit-config.yaml` pointing at a local clone of gelato (`repo: <path>`, `rev: <commit>`, `id: gelato`) builds the hook with `pre-commit install-hooks` and blocks a commit adding a fragment-built secret.
- [ ] `.working-docs/session/parity-jarvis-registry.md` exists, with every entropy difference explained.
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `LICENSE` | MIT, Ke Xue |
| `README.md` | Usage, config, differences, migration |
| `.pre-commit-hooks.yaml` | `gelato` hook definition |
| `.goreleaser.yaml` | Builds, archives, checksums |
| `.github/workflows/release.yaml` | Tag-triggered GoReleaser release |
| `scripts/third-party-licenses.sh` | Generate `THIRD_PARTY_LICENSES` |
| `scripts/parity.py` | Compare tartufo and gelato findings |
| `.gitignore` | Ignore generated `THIRD_PARTY_LICENSES` and `dist/` |
| `.working-docs/session/parity-jarvis-registry.md` | Parity run results |

---

## Risk

Unexplained entropy differences in the parity run would mean existing `exclude-signatures` don't carry over; that run is the gate before switching jarvis-registry's hook.

---

## Out of scope / follow-up

- A `test-and-lint` CI workflow running `make all` on pull requests.
- Homebrew distribution. If added, use GoReleaser's `brews:` formula rather than `homebrew_casks:`: casks quarantine unsigned macOS binaries (see `jarvis-registry-cli/CLAUDE.md`, "Release and distribution").
