---
load:
  - .working-docs/spec/004-git-diff-layer.md
  - .working-docs/review/004-report.md
base_branch: main
next_labels:
  C: 1
  M: 2
  m: 3
---

# Session Notes

<!--
  `load:` paths are relative to CWD, not to this file.
  The last entry in `load:` is always the report file. All entries before it are spec or
  spec-like files. The user may manually add more spec-like files between sessions, but
  the last entry is always the report file.
  Record prior session conclusions, open questions, and constraints here.
  Write it as a future-you artifact.
-->

- Start session reviewed head a5f30da; `make all` passed.
- How M1 was reproduced: a temporary `git/zz_probe_test.go` (deleted afterwards) committed `a\n\nb\nc\n`, staged `a1\n\nb1\nc\n`, set `diff.interHunkContext=3` and `diff.suppressBlankEmpty=true`, then called `Staged`. To verify the fix, rerun that probe or check that `TestStagedHostileConfig` now sets both keys.
- How m1 was reproduced: `Open("/no/such/dir")` returned `git version: chdir /no/such/dir: no such file or directory`.
- Checked and ruled out (don't re-flag): `diff.renames=copies` doesn't leak through `-M50%`, which stays rename-only. An inherited `GIT_OPTIONAL_LOCKS` is deduplicated by `os/exec` (last value wins).
- Considered and not reported: a quoted path holding raw invalid UTF-8 gets U+FFFD from `strconv.Unquote`, while an unquoted one keeps its raw bytes. This is too rare to matter.

## Fixes applied on the branch (2026-10-26, at the user's request)

The fixes are committed locally on `feature/004-git-diff-layer`. Check whether they have been pushed before running followup.
- M1 → `de46772` `fix(git): pin --inter-hunk-context=0 on patch commands`. `--inter-hunk-context=0` is added to `patchArgs`, and the same flag goes into the `CLAUDE.md` option list. `TestStagedHostileConfig` gains `gap.txt`, which has two edits around a blank line, and sets `diff.interHunkContext=3` and `diff.suppressBlankEmpty=true`. Without the flag, the test fails with the reproduced `unexpected line ""` error.
- m1 → `8ebc98a` `fix(git): report a missing directory as not a valid git repository`. `git version` now runs with an empty `Repo.root` (the current directory), and `root` is set to `dir` only for `rev-parse`. A missing dir's chdir error therefore takes the existing non-`runError` branch. New test: `TestOpenMissingDirectory`, which fails against the old code.
- m2 → `a34f3e1` `docs(git): document FileChange fields`. Doc comments are added on `Commit`, `Path` and `Lines`.
- Spec 004 is updated: `--inter-hunk-context=0` in the §2 list and rationale, the `git version` cwd and missing-dir wording in §1, and the new config keys in the §5 tests and the acceptance criteria.
