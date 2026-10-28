# Publish gelato (pre-commit hook, GoReleaser binaries and Homebrew tap)

## Background

After specs 1–6, 8 and 9, gelato works but can't be installed: there is no `LICENSE`, `README`, hook manifest or release config, and `cmd/gelato/main.go`'s `version` stays `"dev"`. It is meant to replace tartufo's hook (`tartufo/.pre-commit-hooks.yaml`: `entry: tartufo`, `language: python`, `args: [pre-commit]`, `pass_filenames: false`), whose `pygit2` dependency breaks on Python minor upgrades. Teams must be able to switch without editing `tartufo.toml`.

---

## Changes

### 1. `LICENSE`

MIT License, `Copyright (c) 2026 Ke Xue`.

### 2. Third-party license notices — `scripts/third-party-licenses.sh`

Go binaries statically include their dependencies (kong and go-toml: MIT; `golang.org/x/crypto`, `x/term` and `x/sys`: BSD-3) and the Go standard library (BSD-3), and those licenses require their notices to ship with binaries. The script lists the modules linked into `./cmd/gelato` (`go list -deps -f '{{with .Module}}{{if not .Main}}{{.Path}} {{.Dir}}{{end}}{{end}}' ./cmd/gelato | sort -u`) and concatenates each module's `LICENSE*`/`COPYING*`/`NOTICE*` files (kong ships only `COPYING`), under a heading per module, into `THIRD_PARTY_LICENSES`, followed by a `Go standard library` section with `$(go env GOROOT)/LICENSE`, or `$(go env GOROOT)/../LICENSE` when that is missing (Homebrew's `go` sets `GOROOT` to the keg's `libexec` and moves `LICENSE` to the keg root). It fails if a module, or the standard library, has no license file. The script is committed executable (mode `100755`) so GoReleaser can run it as a hook. Its output is generated at release time (GoReleaser `before` hook), not committed; `/THIRD_PARTY_LICENSES` and GoReleaser's `/dist/` go into `.gitignore`.

### 3. `.goreleaser.yaml` (GoReleaser OSS, config `version: 2`)

- `before.hooks`: `go mod tidy`, `./scripts/third-party-licenses.sh`.
- `builds`: `main: ./cmd/gelato`, `binary: gelato`, `env: [CGO_ENABLED=0]`, `goos: [linux, darwin, windows]`, `goarch: [amd64, arm64]`, `ldflags: -s -w -X main.version={{ .Version }}`.
- `archives`: `tar.gz`, `zip` for Windows, including `LICENSE`, `README.md`, `THIRD_PARTY_LICENSES`.
- `checksum.name_template: checksums.txt`.
- `brews` (one entry), not `homebrew_casks`: casks quarantine installed binaries and Gatekeeper blocks unsigned ones, and gelato's macOS builds aren't signed or notarized (same reasoning as `jarvis-registry-cli/CLAUDE.md`, "Release and distribution"). GoReleaser has fully deprecated `brews` since v2.16 and plans to remove it in v3, so each release logs a deprecation warning; that is expected.
  - `name: gelato`, `repository: {owner: satoseino, name: homebrew-tap, token: "{{ .Env.HOMEBREW_TAP_TOKEN }}"}`, `directory: Formula`.
  - `homepage: https://github.com/satoseino/gelato`, `description: "Secret scanner for git repositories, compatible with tartufo config."`, `license: "MIT"`.
  - `skip_upload: auto`, so pre-release tags (`v1.0.0-rc.1`) don't update the tap.
  - `extra_install: prefix.install "THIRD_PARTY_LICENSES"`. Homebrew copies `LICENSE` and `README.md` into the keg automatically, but not this file, and the BSD-3 notices have to ship with the binary.
  - `test: system "#{bin}/gelato", "--version"`.
- No code signing.

### 4. `.github/workflows/release.yaml`

`permissions: contents: write`. On `push` of tags `v*`: `actions/checkout@v4` (`fetch-depth: 0`, needed for GoReleaser's changelog), `actions/setup-go@v5` with `go-version-file: go.mod`, then `goreleaser/goreleaser-action@v7` (`distribution: goreleaser`, `version: "~> v2"` so a future v3 without `brews` isn't picked up, `args: release --clean`) with `env` `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}` and `HOMEBREW_TAP_TOKEN: ${{ secrets.HOMEBREW_TAP_TOKEN }}`. `GITHUB_TOKEN` can't write outside this repository, so the tap push uses `HOMEBREW_TAP_TOKEN`, a repository secret (already set) holding a fine-grained PAT limited to Contents read/write on `satoseino/homebrew-tap`. Same action versions as `jarvis-registry-cli/.github/workflows/release.yaml`.

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
- Install as a pre-commit hook (`repo: https://github.com/satoseino/gelato`, `rev: vX.Y.Z`, `id: gelato`), via Homebrew (`brew install satoseino/tap/gelato`, macOS and Linux), and as a binary from GitHub Releases. Requirement: `git` ≥ 2.30 on `PATH`.
- Usage: `gelato pre-commit`, `gelato scan [<repo-path>]`, `--config`, `--no-color`, `--output-format=text|json` (spec 9), `--version`; exit codes 0 / 1 / 2.
- Supported config keys (spec 1) with an example; unsupported tartufo keys are rejected with an error.
- **Differences from tartufo:**
  - only added lines are scanned;
  - its own default regex rules;
  - findings deduplicated within a file change and, in `scan`, across commits (reported at the oldest commit);
  - file-name findings have different signatures;
  - `pre-commit` detects staged renames, so a renamed file's unchanged lines aren't rescanned;
  - `regex`/`entropy` in the repository's own `tartufo.toml` take effect (tartufo honors them only from `--config`), and an unparsable config file is an error rather than ignored;
  - output shows only the matching added lines, and a line longer than 500 runes is cut to a window around the match (spec 8);
  - JSON output (spec 9) uses tartufo's `found_issues` key names, except that `lines` (the windowed matching lines) replaces `diff` and there is no `branch` key;
  - history covers branches, remote-tracking branches and `HEAD`, with every root and shallow-boundary commit diffed against the empty tree.
- **Migrating from tartufo:** replace the tartufo hook entry in `.pre-commit-config.yaml` with gelato's; keep `tartufo.toml` as is; expect possible new regex findings from the different rule set; list any keys gelato rejects.

---

## What does NOT change

- No Go code changes beyond what release builds need; `main.version` already exists (spec 1).
- No code signing. The test-and-lint CI workflow (`.github/workflows/test-and-lint.yaml`) already exists and is unchanged.
- No edits to `jarvis-registry` itself; its hook switch and the `\\.env\\.example$` pattern fix are done separately in that repository.

---

## Acceptance Criteria

- [ ] `HOMEBREW_TAP_TOKEN=unused goreleaser release --snapshot --clean` (GoReleaser fails on an unset `.Env` key, and a snapshot never publishes) produces six archives (linux, darwin, windows × amd64, arm64) plus `checksums.txt`, each containing `gelato`/`gelato.exe`, `LICENSE`, `README.md` and `THIRD_PARTY_LICENSES`.
- [ ] The same snapshot run writes `dist/homebrew/Formula/gelato.rb` with darwin and linux URLs for both arches, the `THIRD_PARTY_LICENSES` install line and the `--version` test.
- [ ] The snapshot binary reports the snapshot version via `gelato --version`, and `go version -m` on it shows `CGO_ENABLED=0`.
- [ ] `THIRD_PARTY_LICENSES` has a section for every non-main module listed by `go list -deps` for `./cmd/gelato`, plus the Go standard library section.
- [ ] In a scratch repository, a `.pre-commit-config.yaml` pointing at a local clone of gelato (`repo: <path>`, `rev: <commit>`, `id: gelato`) builds the hook with `pre-commit install-hooks` and blocks a commit adding a fragment-built secret.
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `LICENSE` | MIT, Ke Xue |
| `README.md` | Usage, config, differences, migration |
| `.pre-commit-hooks.yaml` | `gelato` hook definition |
| `.goreleaser.yaml` | Builds, archives, checksums, Homebrew formula |
| `.github/workflows/release.yaml` | Tag-triggered GoReleaser release, tap token |
| `scripts/third-party-licenses.sh` | Generate `THIRD_PARTY_LICENSES` |
| `.gitignore` | Ignore generated `THIRD_PARTY_LICENSES` and `dist/` |
| `CLAUDE.md` | Release and distribution: `brews` over casks, tap token (already done) |

---

## Out of scope / follow-up

- Verifying the first tagged release, outside this spec's PR: the tap gets a `Formula/gelato.rb` commit, and `brew install satoseino/tap/gelato && gelato --version` prints the tag's version.
- Migrating to `homebrew_casks` only after macOS builds are signed and notarized, or when GoReleaser v3 removes `brews`.
