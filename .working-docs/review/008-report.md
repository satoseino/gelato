---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/7
changes_requested: [m1]
findings_dismissed: []
merged:
notes:
---
# PR Review: Feature: truncate long report lines to a window around the match

**PR:** https://github.com/satoseino/gelato/pull/7
**Spec:** .working-docs/spec/008-report-truncation.md
**Reviewed:** 2026-10-27

## Summary

The PR follows the spec closely. It adds a small pure `excerpt(line, start, end)` helper that does rune-wise windowing, and an `excerpts(i)` wrapper that maps the anchor span for single-line and multi-line matches. Windowing happens before highlighting, so escape sequences are never counted or cut. `make all` passes. Running the PR build on jarvis-registry reproduces the claimed result: 510 issues, a signature set identical to an `origin/main` build, a 271 KB report, and a longest line of 403 runes. The one gap is missing tests for the multi-line span mapping at line boundaries. The code handles those cases correctly today.

## Spec Compliance

| Requirement | Status |
|---|---|
| `maxLineRunes = 500`, `contextRunes = 80` constants next to `issueSeparator` | Met |
| Rune-based counting and stepping; cuts on rune boundaries | Met (`utf8.RuneCountInString`, `DecodeRuneInString`/`DecodeLastRuneInString`) |
| `…` only on cut sides; match never cut; short lines unchanged | Met |
| Single-line: anchor on first occurrence per body line | Met |
| Multi-line: anchor once in joined chunk, map span onto lines; inner lines never cut; outside lines → `[0, 0)` | Met |
| Window before highlighting | Met (`excerpts` replaces `bodyLines` before `ReplaceAll`) |
| Single pure helper, table-testable | Met (`excerpt`) |
| `issue` doc comment updated | Met |
| `TestExcerpt` table covering max length, empty anchor, start/middle/end, oversized match, multi-byte | Met |
| `long.txt` / `long-multiline.txt` fixtures | Met |
| Long-line `TestReporterColor` case | Met (two cases: single-line and multi-line) |
| Existing fixtures byte-identical | Met (no existing testdata changed; `make all` passes) |
| jarvis-registry: 510 issues, same signatures, < 2 MB, bounded line length | Met (verified locally: 510 issues, `diff` of sorted signatures vs `origin/main` build is empty, 270,805 bytes, max line 403 runes) |
| `make all` passes | Met |

## Findings

### Critical

None.

### Major

None.

### Minor

**[m1] Multi-line span mapping has no tests at line boundaries** — In `excerpts` (`command/report.go`), the overlap test `offset <= end && lineEnd >= start` is inclusive on both sides. This is deliberate: a match that starts with `"\n"` anchors the previous line at an empty span at its end, and a match that ends with `"\n"` anchors the next line at `[0, 0)`. A scratch probe confirmed that matches `"\nmid"`, `"mid\n"` and `"\nmid\n"` over long neighbouring lines all keep the match contiguous in the windowed body. Matches like these are reachable: `detect/engine.go:108-116` runs user regexes over `strings.Join(chunk.Lines, "\n")`, so a `regex` rule such as `\nfoo` can produce one. However, the only multi-line fixtures (`long-multiline.txt` and the `long multiline` color case) use a match that starts and ends mid-line. If someone later tightens `lineEnd >= start` to `>`, a match that starts at a `"\n"` would leave its preceding line in the `[0, 0)` branch. That cuts the line's end out of the window, so `ReplaceAll` silently stops highlighting, which is the regression the spec's Risk section names, and no test would fail. (Tightening `offset <= end` is behavior-preserving, since an anchor at `offset == end` is `[0, 0)`, the same as the fallback.) Suggested fix: add a small table test for `excerpts` with matches that start and end with `"\n"` next to lines longer than `maxLineRunes`.

*Fix status (local, commit `4230afa`, not yet pushed):* this adds `TestExcerptsLineBoundary` in `command/report_test.go`, covering matches inside lines, starting at `"\n"`, ending at `"\n"`, and both. A mutation check confirmed it fails when `lineEnd >= start` is tightened. `make all` passes.

## Positive Observations

- `excerpt` is minimal and correct. It returns early on short lines, steps rune by rune in both directions, and adds markers only when `from > 0` / `to < len(line)`. Invalid UTF-8 is handled consistently, since both counting and stepping treat a bad byte as one rune.
- The multi-line mapping matches the spec's design: lines strictly inside the match get a whole-line anchor and are never cut, so `highlight`'s per-segment styling keeps working unchanged.
- The tests are well targeted. The multi-byte case (`é` = 2 bytes, `日` = 3 bytes) would fail under byte counting, every case checks `utf8.ValidString`, and the color cases pin `…` outside the escape sequences.
- The change is confined to display: `detect.Issue`, `bodyLines`'s selection rule, config and call sites are untouched. The identical jarvis-registry signature set confirms this end to end.

## Recommendation

**Approve** — the implementation meets every acceptance criterion, including the end-to-end jarvis-registry check. The one finding is an optional regression test for line-boundary matches.
