---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/6
changes_requested: [M1, m1, m2]
findings_dismissed: []
merged:
notes:
---
# PR Review: Feature: implement gelato scan over the repository's history

**PR:** https://github.com/satoseino/gelato/pull/6
**Spec:** .working-docs/spec/006-scan-command.md
**Reviewed:** 2026-10-27

## Summary

The PR implements spec 006 closely. It lists commits with one `rev-list`, runs one `diff-tree --stdin` per contiguous batch, shares a chunk pipeline between `scan` and `pre-commit`, deduplicates findings at the oldest commit and reads commit metadata with one `git log` call. The concurrency is correct: each batch writes only to its own slots, issue slots are read only after `p.wait()`, and cancellation is handled. `make all` passes (tests run with `-race`). The main issue is that `Commit time` uses `%cI`, whose UTC output changed in git 2.45. The output and the tests therefore depend on the git version, even though gelato's minimum supported git is 2.30.

## Spec Compliance

| Spec item | Status | Notes |
|---|---|---|
| §1 `Commits`: `rev-list --topo-order --parents --branches --remotes [HEAD]`, HEAD only when `rev-parse --verify -q` succeeds | Met | `git/history.go:37-77` |
| §1 Root and shallow-boundary commits get `FirstParent == ""` | Met | `TestCommitsRefs`, `TestPatchesShallow` |
| §2 `Patches`: one `diff-tree --stdin --root`, `<id> <parent>` form, `FileChange.Commit` set | Met | `TestPatchesMerge` checks the merge's diff against its first parent |
| §3.1 `git.Open(..., WithoutRepoEnv())` | Met | |
| §3.3 Empty repository prints "All clear" | Met | Goes through `summary(0)` instead of an early return; same result |
| §3.4 `min(GOMAXPROCS, len)` contiguous batches, run concurrently | Met | |
| §3.5 Shared chunk conversion and pool in `command/pipeline.go`, used by both commands | Met | |
| §3.6 / §4 Dedup on `(Signature, Type, Detail)` at the highest rev-list index; output in rev-list, then chunk, then issue order | Met | `dedup` keeps the first occurrence within the oldest commit |
| §5 `CommitInfo` through one `git log --no-walk=unsorted --stdin --no-show-signature --encoding=UTF-8` | Partially Met | Implemented, but `%cI` output depends on the git version (see [M1]) |
| §6 Tests: add-then-remove, merge, remote/detached/two roots, shallow, empty, skipped, repo-path sources, repeated runs | Met | All present. The repeated-run test uses a single batch layout (see [m1]) |
| AC: jarvis-registry wall time recorded in spec 7 parity notes next to tartufo's | Partially Met | gelato's time (93–100s) is in the PR body. tartufo's time and the spec 7 notes are still pending |
| AC: `make all` passes | Met | Checked locally with git 2.56.0 |

## Findings

### Critical

_None._

### Major

**[M1] `Commit time` format depends on the git version, and tests fail on git 2.30–2.44** — `git/history.go:109` formats the time with `%cI` (strict ISO 8601). In git 2.45 (commit 69e2bee, "date: make iso-strict conforming for the UTC timezone"), a zero offset changed from `+00:00` to `Z`. gelato's `minVersion` is 2.30.0, and Ubuntu 24.04 (2.43) and Debian 12 (2.39) ship versions in the affected range. As a result:
- `TestCommitInfo` and the `added-removed.txt.tmpl` golden file both expect `2020-01-01T00:00:00Z`, so `make all` fails on any supported git older than 2.45. CI passes only because `ubuntu-latest` ships a newer git.
- The same repository gives different `Commit time` text depending on the user's git version, which goes against the PR's byte-identical-output goal across machines.
Fix: read the date raw and build the time in Go as RFC 3339 in the committer's offset. Go writes `Z` for a zero offset, so the output matches git 2.45+ `%cI` on every git version and the existing goldens stay valid. (`--date=format:...%z` gives a non-strict `+0000`, and raising `minVersion` to 2.45 would drop distro gits.) Note: the fix originally agreed in triage named `%ct`/`%cz`, but git has no `%cz` placeholder; `%cd` with `--date=raw` (`<unix> <+hhmm>`) provides the same two values.

*Status:* fixed on the PR branch in 85a9570. `CommitInfo` uses `--date=raw --format=%H%x00%cd%x00%s` and formats it with `commitTime`; `TestCommitInfo` adds a `+05:30` committer and `TestCommitTime` covers the parsing table. Awaiting follow-up verification.

### Minor

**[m1] The determinism test never changes the batch layout** — `command/scan_test.go:257` fixes `GOMAXPROCS(4)` and compares five runs that use the same four batches. Spec §4 promises the same output "regardless of batch timing". The real cross-machine risk is a different *batch count*: a user with 8 cores versus CI with 2 changes where commits are split and which goroutine reports a merge's re-shown lines first. Comparing output at `GOMAXPROCS(1)` with `GOMAXPROCS(4)` (and with a count that doesn't divide 30, such as 7) costs little and covers the batch-boundary and attribution risk named in the spec's Risk section.

*Status:* fixed on the PR branch in b9d4840. `TestScanDeterministic` compares GOMAXPROCS 1 against 4, 4, 7 and 32 (one commit per batch) and adds a merged side-branch secret that must stay attributed to the side commit. Awaiting follow-up verification.

**[m2] A change with an unknown commit is silently attributed to the newest commit** — `command/scan.go:141` uses `index[fc.Commit]`. If `fc.Commit` is missing from the map (for example `""` because the parser saw no object-ID line, or an unexpected ID form), the lookup returns `0`, the newest commit. That issue then gets the wrong `Commit hash`/`time`/`message` and wins or loses dedup against the wrong index. Nothing is reported. Following the repository's fail-loudly approach, the batch should return an error (e.g. `diff-tree printed a change for unknown commit %q`) when the lookup fails.

*Status:* fixed on the PR branch in 4b3acfc. The batch now stops with `git diff-tree printed <path> for unknown commit "<id>"` and cancels the other batches. No test was added: there is no existing seam to inject such a change, and the repo's testing rules forbid adding one just for this branch. Awaiting follow-up verification.

## Positive Observations

- One `rev-list` replaces tartufo's per-branch walk and MD5 dedup, and it fixes tartufo's gaps: only one root scanned, remote-only and detached commits missed, and shallow clones special-cased.
- The `<commit> <first-parent>` stdin form is the right way to get a merge diff from `diff-tree`. `TestPatchesMerge` locks this in.
- Determinism holds by construction: batches are contiguous, each batch keeps its slots in order, and the batches are joined in index order. No sort after the fact is needed.
- `firstError` prefers the real failure over the cancellations it caused in other batches, and `run` checks `ctx.Err()` before `waitErr`, so a killed `diff-tree` doesn't produce a misleading parse error.
- The tests are thorough and end-to-end on real repositories. `TestScanRepoPath` points `GIT_DIR` at the *other* repository in each case, which is a strong check of `WithoutRepoEnv`.
- The `pre-commit` refactor keeps its ordering and calls `p.wait()` on the error path, so no worker goroutines are leaked.

## Recommendation

**Request Changes** — [M1] makes `make all` fail on git versions gelato says it supports, and it makes the report text depend on the git version. The rest is small hardening.
