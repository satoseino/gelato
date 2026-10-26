---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/2
changes_requested: [m1, m2, m3]
findings_dismissed: []
merged:
notes:
---
# PR Review: Feature: add git-free regex and entropy detection engine

**PR:** https://github.com/satoseino/gelato/pull/2
**Spec:** .working-docs/spec/002-detection-engine.md
**Reviewed:** 2026-10-26

## Summary

A small, faithful port of tartufo's detection behavior into a git-free `detect` package. I checked it against `tartufo/tartufo/scanner.py` and `util.py` and against Python 3.15 directly: every signature and entropy golden in `detect/testdata/` matches Python exactly, the base64 class's non-ASCII members (`U+0130`, `U+0131`, `U+017F`, `U+212A`) and Python's `str.isspace` set match the Go predicates, and the check order (signature, then entropy, then the line+path exclusion with search semantics) mirrors `evaluate_entropy_string`. `make all` passes, including `-race`. The only findings are minor: a concurrency-safety claim that rests on caller discipline, an ordering doc that is slightly wrong, and a self-scan follow-up that isn't tracked anywhere.

## Spec Compliance

| Requirement | Status |
|---|---|
| §1 Types `Chunk`, `IssueType`, `Issue`, `Rule` | Met |
| §2 `Signature` = hex blake2s of `match$$path`, Python golden | Met (all 7 goldens re-verified with `hashlib.blake2s`) |
| §2 Exclusion by raw string or signature | Met |
| §3 Rules sorted by name; >1 capture group rejected | Met (also rejects a nil `Pattern`) |
| §3 Engine immutable / safe for concurrent use | Partially Met — see [m1] |
| §4.1 Path exclusion short-circuits both scans | Met |
| §4.2 Regex over `\n`-joined lines, group 1 vs whole match, start-anchored `PathPattern` | Met |
| §4.3 Entropy: Python whitespace split, base64/hex classes, ≥20 runes, strict `>` 4.5 / 3.0, signature first, line+path exclusion | Met |
| §4.4 Within-chunk dedup on `(Type, Detail, Match)` | Met |
| Deterministic order (regex by rule then position, entropy by line then position) | Met in behavior; the doc comment overstates it — see [m2] |
| §5 Rune-based length and entropy | Met |
| §6 Tests (signature, entropy goldens and boundary, tokenizing, exclusions, regex, toggles, race) | Met |
| AC: `make all` incl. `-race` | Met (ran locally) |
| Golden inputs never token-shaped | Met for inputs; digests in outputs need a self-scan exclusion — see [m3] |

## Findings

### Critical

_None._

### Major

_None._

### Minor

**[m1] `Engine` is "immutable" only if the caller never touches the `cfg.Config` it passed in** — `New` stores `c cfg.Config` by value (`detect/engine.go:69`), but `ExcludeSignatures` (a map), `ExcludePaths` and `ExcludeEntropyPatterns` (slices) still alias the caller's storage. The doc comment (`detect/engine.go:17-18`) and spec §3 promise an engine that is immutable after `New` and safe for concurrent use. If a caller writes to `c.ExcludeSignatures` while workers are calling `Scan`, that is a data race on a map, and `-race` would not catch it because the current test never mutates the config. Suggested fix: clone the three containers in `New` (`maps.Clone`, `slices.Clone`), the same way `rules` is already cloned. That makes the guarantee hold no matter what the caller does.

**[m2] `Scan`'s doc says entropy issues come "by line then position", which isn't true within a word** — `scanEntropy` (`detect/engine.go:125-133`) emits all base64 candidates of a word before any of its hex candidates, as tartufo does. For a word `B1 + "." + B2` where `B1` contains a qualifying hex run `H1`, the order is `B1, B2, H1`, so `H1` comes after `B2` even though it starts before it. The behavior is correct and deterministic. Only the public contract in the `Scan` doc comment (`detect/engine.go:72-73`) is wrong. Suggested fix: reword it to "entropy issues by line, then word, base64 candidates before hex", matching the `scanEntropy` comment.

**[m3] The self-scan exclusion for committed digests isn't tracked, and the PR note covers only one of the two files** — The PR description says `detect/testdata/signatures.json` will need an `exclude-path-patterns` entry. But `detect/signature_test.go:12` also contains a 64-hex digest literal, and random 64-hex strings have entropy around 3.8, above the 3.0 hex limit. No later spec (003–007) mentions a repository `tartufo.toml` or self-scan exclusion, so the follow-up depends on someone remembering this PR note. Suggested fix: name both paths in the note and add the exclusion to a later spec (spec 7, distribution/migration, is the natural home), or commit a minimal `tartufo.toml` with the exclusion now.

## Positive Observations

- Parity details are handled carefully and backed by evidence: insertion-order summation like Python's `Counter`, an explicit `float64(...)` conversion to prevent FMA fusion, at-limit fixtures built so the arithmetic is exact in both languages, and a documented ulp tolerance for the rest. In practice the goldens match bit for bit.
- `matchesAtStart` correctly turns Go's leftmost-first matching into `re.match` semantics without recompiling the pattern.
- High-entropy fixtures are built at runtime from rune ranges, so no token-shaped literal is committed.
- `New` clones the caller's rule slice before sorting, and a test checks that the caller's order is untouched.
- Signature exclusion runs before the entropy calculation, matching tartufo's `scanner.py:645`.

## Recommendation

**Approve** — the implementation meets the spec and its parity claims hold against Python; the three minor findings are cheap hardening and documentation fixes.
