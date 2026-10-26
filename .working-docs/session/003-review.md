---
load:
  - .working-docs/spec/003-default-rule-set.md
  - .working-docs/review/003-report.md
base_branch: main
next_labels:
  C: 1
  M: 1
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

## Start session (2026-10-26)

- Verified locally: `make all` passes; the acceptance `git grep` finds nothing; all 21 patterns match the spec table verbatim.
- [m1] scope, set by the reviewer: add only `%` and `*` to `Password in URL`'s excluded first characters (`[^\s:/@$<{%*]`), with negative cases. **Literal placeholders such as `password` must keep matching.** In follow-up, flag a fix that also skips placeholder words as going beyond the requested change. The spec's template note (line 48) should change to match.
- [m2] is addressed by a positive Twilio case that uses uppercase hex.
- Checked and not raised:
  - GitLab routable tokens (`glpat-…<dot>…`) still match, but only the part before the first `.`.
  - `xoxe.xoxp-` config tokens match the inner `xoxp-`.
  - An OpenAI/Anthropic double match needs a contrived string with `T3BlbkFJ` inside an `sk-ant-` key.
  - Signatures differ from tartufo's for rules with context. The spec chose that design ("signature covers only the secret").

## Fixes applied on the branch (2026-10-26, at the user's request)

The fixes were committed locally on `feature/003-default-rule-set`. Check whether they have been pushed before running followup.
- m1 → `d677e0a` `fix(detect): skip masked and printf-style passwords in URLs`: the password's first character now excludes `$<{%*`. New negative cases `printf template` and `masked`, plus a positive `placeholder word` case that pins literal `password` as still matching, per the reviewer's scope. Against the old pattern, the two negatives fail.
- m2 → `84d40ab` `test(detect): cover uppercase hex in Twilio API keys`: a positive `uppercase hex` case. With the pattern narrowed to `[0-9a-f]`, it fails.
- The new URL fixtures split the password into its own string fragment, like the existing `database URL` case. As a single literal, gosec G101 flagged them; no lint exclusion was added.
- Spec 003 now has the new pattern and template note, the extra negatives under section 2, and the uppercase-hex test.
- `make all` and the acceptance `git grep` both pass after the fixes.
