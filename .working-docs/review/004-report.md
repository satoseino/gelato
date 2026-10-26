---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/4
changes_requested: [M1, m1, m2]
findings_dismissed: []
merged:
notes:
---
# PR Review: Feature: add git CLI layer with pinned invocations, patch parser and staged diff

**PR:** https://github.com/satoseino/gelato/pull/4
**Spec:** .working-docs/spec/004-git-diff-layer.md
**Reviewed:** 2026-10-26

## Summary

A careful, faithful implementation of spec 4. The runner pins every option the spec lists, the parser reads hunks by count, and the fixtures were captured from real git, covering nearly every case the spec names. `make all` passes. The one substantive gap is a pair of diff config keys the pinned options miss. Together they make `ParsePatch` hard-fail on ordinary content, which is the risk the spec's "Risk" section anticipates.

## Spec Compliance

| Requirement | Status |
|---|---|
| §1 `Open`: LookPath error, version ≥ 2.30 with suffix parsing, `rev-parse --show-toplevel`, tartufo error wording | Met (see [m1] for a nonexistent dir) |
| §1 Environment: inherit by default, `WithoutRepoEnv` strips the `--local-env-vars` list, `GIT_OPTIONAL_LOCKS=0` | Met |
| §2 Pinned `-c` args and patch args, streamed stdout, optional stdin, stderr in `git <sub> failed: …`, context cancel | Met. The pinned list is incomplete in practice (see [M1]) |
| §3 Parser: `bufio.Reader`, object-ID commit lines (SHA-1/256), path sources and decoding, TAB strip, type change → `Added`, hunk counting, `ToValidUTF8`, dropped entries | Met |
| §4 `Staged` incl. unborn branch | Met |
| §5 Tests: patch fixtures, `internal/gittest`, end-to-end cases | Met. All listed fixture kinds are present (`--- dashed` removed line in `history.patch`) |
| §6 Scoped G204 exclusions | Met (text regex is `G204: Subprocess launched with`, slightly broader than the spec's `…with variable`, still scoped by path) |
| §7 `CLAUDE.md` updates | Met |
| AC: non-repo error, hostile config, unborn, binary/deleted/submodule dropped, tricky path round-trip, `GIT_INDEX_FILE` | Met |
| AC: `make all` passes | Met |
| Extra (not in spec) | `tartufo.toml` path exclusion for `git/testdata/*.patch|json` (object IDs). Reasonable |

## Findings

### Critical

### Major

**[M1] `diff.interHunkContext` + `diff.suppressBlankEmpty` repo config makes `Staged` fail** — `patchArgs` (`git/repo.go:57-61`) doesn't pin `--inter-hunk-context`. With `diff.interHunkContext=N`, git merges `-U0` hunks less than N lines apart and puts the lines between them in as context lines. With `diff.suppressBlankEmpty=true`, a blank context line is printed as an empty line instead of `" "`. `hunk()` (`git/patch.go:267-280`) only accepts `+`, `-`, ` ` and `\` lines, so it rejects the empty line. I reproduced this with a temporary probe test: committing `a\n\nb\nc\n`, staging `a1\n\nb1\nc\n` and setting both keys makes `Staged` return `git diff: unexpected line "" in hunk "@@ -1,3 +1,3 @@"`. In the pre-commit hook, every commit that touches such a file then fails. Both keys are plain user/repo config, and this is the exact risk the spec's Risk section names. Fix: add `--inter-hunk-context=0` to `patchArgs` (git ≥ 2.12, well below the 2.30 floor), which makes `suppressBlankEmpty` irrelevant under `-U0`. Optionally also pin `-c diff.suppressBlankEmpty=false` in `configArgs`. Add both keys to the `TestStagedHostileConfig` loop, and update the pinned-option list in `CLAUDE.md` and the spec.

### Minor

**[m1] `Open` on a nonexistent directory doesn't return the "not a valid git repository" error** — `Open` sets `r.root = dir` before running `git version` (`git/repo.go:112-118`), so `cmd.Dir` is the target directory even for the version check. If `dir` doesn't exist, `cmd.Start` fails and `Open` returns `git version: chdir /no/such/dir: no such file or directory` (I reproduced this). That error is about the directory, but it's reported as a git-version failure and skips tartufo's wording. tartufo rejects a missing path earlier, with `click.Path(exists=True)`, and gelato's `scan` `RepoPath` has no equivalent check. Either run `git version` without `Dir`, or turn a start failure in `rev-parse` into `<dir> is not a valid git repository: …`.

**[m2] `FileChange` fields lost their documentation** — The spec defines `Commit` as "object ID from a diff-tree --stdin header line; "" for staged diffs" and `Path` as "new path, repository-relative". The PR (`git/patch.go:17-23`) keeps the fields with no comments, and the type's doc line only says "one file's added lines from a patch". Spec 5 and spec 6 callers will rely on those semantics (empty `Commit` for staged changes, new path for renames). A one-line comment per field would state them where callers look.

## Positive Observations

- Hunks are read by count, and both the `+++ plussed` and `--- dashed` traps are captured from real git (`history.patch`), so the parser's main hazard is exercised directly.
- `run` is small and correct around early stop and errors. Breaking out of iteration cancels the process, a parse error drains stdout and prefers git's own error, and context cancellation is reported as `ctx.Err()`. Each path has a test (`TestStagedStopsEarly`, `TestStagedGitFailure`, `TestStagedCanceled`).
- `TestStagedHostileConfig` builds every change kind in one repo, including a real gitlink bump via `update-index --cacheinfo`. It then reruns from a subdirectory, which also covers `diff.relative`.
- `gittest.New` clears inherited repo-routing variables, so the suite stays correct when `go test` itself runs from a git hook.
- `gitHeaderPath`'s midpoint split is tested on the adversarial `a/ b/ b/ b/` header.

## Recommendation

**Request Changes** — [M1] lets ordinary user/repo config break every pre-commit scan of affected files, and the fix is a one-flag change; the minors are optional polish.
