---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/3
changes_requested: [m1, m2]
findings_dismissed: []
merged:
notes:
---
# PR Review: Feature: add gelato's own default regex rule set

**PR:** https://github.com/satoseino/gelato/pull/3
**Spec:** .working-docs/spec/003-default-rule-set.md
**Reviewed:** 2026-10-26

## Summary

The PR adds `detect.DefaultRules()`: 21 rules that match the spec's table exactly, including the three documented corrections (AWS secret terminator, Twilio uppercase hex, PyPI 70+ body). Each rule links its provider documentation. The tests build their fixtures from fragments, run every rule through `detect.New`, and fail when a rule lacks a positive or a negative case. `make all` passes and the acceptance `git grep` finds nothing. There are no blocking issues, only two minor gaps: a false-positive class in `Password in URL` and an untested pattern change.

## Spec Compliance

| Requirement | Status |
|---|---|
| `DefaultRules()` returns a copy of package-level `regexp.MustCompile` literals; no data file | Met (`slices.Clone`, `detect/rules.go:147`) |
| At most one capture group per rule; group marks the secret | Met (`New` guard plus `TestDefaultRulesWellFormed`) |
| Each rule cites provider documentation | Met |
| `PathPattern` nil for every rule | Met (asserted in `TestDefaultRulesWellFormed`) |
| Patterns match the spec table (as updated) | Met: all 21 compared verbatim |
| `Password in URL` skips `$`, `<`, `{` templates | Met (see [m1] for template forms it doesn't cover) |
| Positive and negative table case per rule via `detect.New` | Met, and enforced by the test itself (`detect/rules_test.go:205`) |
| Fixtures assembled from fragments; acceptance `git grep` finds nothing | Met (ran locally: exit 1, no matches) |
| Required negatives (wrong prefix, one short, `${…}`/`<…>`, username-only, `AC…` SID) | Met |
| Name uniqueness and group-count test | Met |
| Mutating the returned slice doesn't affect later calls | Met (`TestDefaultRulesReturnsCopy`) |
| `CLAUDE.md` line updated | Met: identical to the spec's wording |
| `make all` passes | Met (ran locally) |

## Findings

### Critical

None.

### Major

None.

### Minor

**[m1] `Password in URL` flags masked and printf-style placeholder passwords** — The rule excludes only passwords that start with `$`, `<` or `{`. Other common non-secret forms still match. I confirmed these through `detect.New(…, DefaultRules())`:
- `https://user:********@host/db` → match `********` (masked credentials in docs and logs)
- `https://user:%(password)s@host/db` → match `%(password)s` (Python `%`-format settings templates)
- `https://user:password@example.com/db` → match `password` (README placeholder)

In a pre-commit hook, each of these blocks the commit until the user adds an `exclude-signatures` entry. The spec's Risk section names this tradeoff ("A loose pattern blocks commits with false positives"). Requested fix: add `%` and `*` to the excluded first characters (`[^\s:/@$<{%*]`), add negative cases for the masked and `%`-format forms, and update the spec's `Password in URL` note to match. Literal placeholder words such as `password` should still match; this change should not skip them.

**[m2] Twilio's uppercase-hex change has no test** — The PR widens the Twilio pattern from `[0-9a-f]` to `[0-9a-fA-F]` and lists this as one of three deliberate corrections. Every Twilio fixture uses `lowerHex` (`detect/rules_test.go:49`, `:131`, `:133`), so reverting to `[0-9a-f]` would still pass. The other two corrections each have a test that would catch a revert: `ends in slash` for AWS and `one rune short` for PyPI. A fix is a positive case with uppercase hex digits (for example `"S" + "K" + cycle(digits+runeRange('A', 'F'), 32)`).

## Positive Observations

- `TestDefaultRules` checks that every rule has both a positive and a negative case (`detect/rules_test.go:205`), so a new rule added without tests fails CI instead of relying on review.
- The fixtures are well chosen: alphabet helpers (`cycle`, `base32`, `urlSafe`) make the lengths readable, and negatives target each pattern's real boundaries, such as `AIDA` IAM user IDs, `gldt-` deploy tokens, Stripe `pk_live_`/`sk_test_`, the Twilio `AC` SID and unpadded Azure keys.
- Each assertion filters by `Detail == tt.rule`, so the cases exercise the engine's group-1 extraction without coupling to other rules or to entropy.
- The AWS secret terminator `(?:[^A-Za-z0-9/+]|$)` fixes a real gap in the spec's original `\b` version (keys ending in `/` or `+`), and the spec was updated in the same PR to keep it authoritative.
- The rule comments state *why* (for example, test-mode Stripe keys are excluded because they grant no access to real data) rather than restating the regex.

## Recommendation

**Approve** — The PR meets every acceptance criterion. [m1] and [m2] are small precision and test-coverage improvements that can land here or in a follow-up.
