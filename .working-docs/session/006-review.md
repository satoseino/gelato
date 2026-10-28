---
load:
  - .working-docs/spec/006-scan-command.md
  - .working-docs/review/006-report.md
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

- Start review on 2026-10-27 at HEAD 0941473 (2 commits on top of main). `make all` passed locally with git 2.56.0.
- [M1] fix agreed with the reviewer: format the date in Go with `time.RFC3339` (Z for UTC). This matches git ≥2.45 `%cI`, so the existing goldens (`2020-01-01T00:00:00Z`) should stay unchanged. Background: git 2.45 commit 69e2bee changed iso-strict UTC to `Z`; gelato's `minVersion` is 2.30. The triage text said `%ct`/`%cz`, but `%cz` doesn't exist in git; the implementation uses `--date=raw` + `%cd` instead. Spec §5 was updated to match.
- [m1] expects the determinism test to compare output across *different* GOMAXPROCS values (e.g. 1, 4 and 7 on 30 commits), not only repeated runs at 4.
- [m2] expects `scanHistory` to return an error when `fc.Commit` is not in `index`, instead of defaulting to 0.
- Checked and found fine: the concurrency (each batch owns `slots[b]`, issues are read after `p.wait()`), `firstError` preferring the real error over cancellations, `run` checking `ctx.Err()` before `waitErr`, and `diff-tree --stdin` printing a bare commit ID for the `<commit> <parent>` form.
- AC open outside this PR: tartufo's jarvis-registry wall time in the spec 7 parity notes (gelato: 93–100s on 8 cores). It was not raised as a finding.
- 2026-10-27, at the user's request, Claude fixed all three findings directly on the PR branch: [M1] 85a9570, [m1] b9d4840, [m2] 4b3acfc. `make all` passes after each commit (git 2.56.0). The labels stay in `changes_requested` until a `followup` session confirms each against its posted reviewer comment. Each finding in the report has a *Status* line naming its commit.
