---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/5
changes_requested: [m2]
findings_dismissed: [m1]
merged:
notes:
---
# PR Review: Feature: implement gelato pre-commit with tartufo-style text report

**PR:** https://github.com/satoseino/gelato/pull/5
**Spec:** .working-docs/spec/005-pre-commit-command.md
**Reviewed:** 2026-10-27

## Summary

The PR implements the spec faithfully. It wires `git.Repo.Staged`, `detect.Engine` and a new shared `reporter` into `PreCommit.Run`, adds a deterministic worker pool and `--no-color`/`NO_COLOR` handling, and includes the real-hook test the spec's Risk section calls for. `make all` passes locally. I found no correctness bugs. Both findings concern test coverage and test-harness cost: the colour-off paths are never checked in CI, and every test run in `command` builds the binary.

## Spec Compliance

| Requirement | Status |
|---|---|
| §1 `Globals.NoColor`, `kong.BindTo(stdout, (*io.Writer)(nil))`, colour only on non-`NO_COLOR` TTY `*os.File` | Met |
| §2 `reporter` layout (Reason/Detail/Filepath/Signature/meta/body), green labels, yellow match | Met |
| §2 Body = added lines containing the match; whole chunk for multi-line matches | Met |
| §2 `Found <n> issue(s).` / `All clear. No secrets detected.` | Met |
| §3.1 `git.Open(".")` with inherited env (hook's `GIT_INDEX_FILE`) | Met |
| §3.3 Content chunk + separate path chunk for `Added`/`Renamed` | Met |
| §3.4 `GOMAXPROCS` workers, sequence-ordered reassembly | Met |
| §3.5 `ErrFindings` on issues, errors propagate | Met |
| §4 E2E tests: clean, regex+entropy, exclusions (raw/sig/path), file name, added-only, unborn, rename, determinism, real hook, forced colour | Met |
| §4 Scoped `G204` exclusion | Met (it also covers `G302` for the hook `chmod`, and says so in a comment) |
| AC: no ANSI escapes when piped / `--no-color` / `NO_COLOR=1` | Partially Met — see [m1] |
| AC: `make all` passes | Met (verified locally) |

## Findings

### Critical

None.

### Major

None.

### Minor

**[m1] `--no-color` and `NO_COLOR` are never verified in CI** — `TestUseColor` (`command/report_test.go`) checks `--no-color` and `NO_COLOR=1` only after it opens `/dev/tty`, and skips otherwise. The GitHub Actions runner that runs `make test` has no controlling terminal, so those assertions never run in CI. The buffer and pipe cases can't fill the gap: color is already off for a non-terminal writer, so a regression that ignored `--no-color` or `NO_COLOR` would still pass them. The acceptance criterion "no ANSI escapes … with `--no-color`, or with `NO_COLOR=1`" is therefore checked only on developer machines.

**[m2] `TestMain` builds the gelato binary for every test in package `command`** — `command/precommit_test.go`'s `testMain` runs `go build` before `m.Run()`, so it runs even for `go test -run TestReporter`. If the build fails (e.g. `go` isn't on `PATH`, or the test binary was built with `go test -c` and run elsewhere), every test in the package exits with code 2, including the pure reporter tests. Only `TestPreCommitHook` needs the binary. Suggested fix: build it lazily inside that test, or behind a `sync.Once` helper it calls, using `t.TempDir()`. A build failure then fails only that test.

*Fixed in 7874cdb:* `TestMain` now only resolves `testdata`; a `buildGelato(t)` helper, called only by `TestPreCommitHook`, builds into that test's `t.TempDir()`. `make all` passes. Left in `changes_requested` so the followup can confirm it against the posted comment.

## Positive Observations

- `TestPreCommitHook` checks the main risk the spec names: it uses `git commit -a` so that `.git/index` never holds the secret, and it asserts that HEAD did not move.
- The reporter's `highlight` styles each line of a multi-line match separately, so every body line keeps its `+` prefix outside the escape codes. A forced-color test pins this down.
- `scanChunks` writes results by sequence index into a pre-sized slice, so it needs no locks and output order doesn't depend on scheduling. A 50-file, 6-run determinism test covers it.
- Golden files are templates rendered with secrets assembled at runtime, so the repo contains no token-shaped strings, as `CLAUDE.md` requires.
- `chunksOf` keeps the path as its own chunk, so a finding in a file name shows only the path in its body, as the spec says.
- The `//nolint:govet` removal on `cli` is legitimate: the directive became unused, and lint passes.

## Recommendation

**Approve** — the implementation matches the spec and passes `make all`. Both findings are minor test-coverage and test-harness improvements.
