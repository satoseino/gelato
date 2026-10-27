# Truncate long report lines to a window around the match

## Background

`reporter.issue` (`command/report.go:44`) prints every added line that contains the match (`bodyLines`, `command/report.go:116`) in full. When an added line is huge, the whole line goes into every finding on it. Scanning `jarvis-registry` (510 findings) produced a 92 MB report, and about 80 MB of it came from 76 findings on just 4 distinct lines of a minified MkDocs `search/search_index.json` (lines of 0.99–1.42M runes) on `origin/gh-pages`. Each of those findings repeated its full 1 MB line, while its match was 24–179 runes long. The matches sit far apart within a line (e.g. offsets 44K → 959K in one line), so the only usable fix is per finding. With `--no-color` the match isn't highlighted, so finding it inside a 1 MB line is impractical. Decision: cut each long body line down to a window around the match. Findings stay ungrouped.

---

## Changes

### 1. Window long body lines in `reporter.issue`

In `command/report.go`, add two unexported constants next to `issueSeparator` (`command/report.go:27`):

- `maxLineRunes = 500`: a body line longer than this many runes is cut down to a window.
- `contextRunes = 80`: how many runes of context to keep on each side of the match.

Count line lengths and context in runes (`utf8.RuneCountInString` and rune-wise stepping), not bytes, as gelato does everywhere else (see CLAUDE.md, "string lengths and entropy count runes"). This also means a cut never splits a multi-byte character.

For each body line longer than `maxLineRunes`, keep:

```
[…]<up to contextRunes runes before the match><match><up to contextRunes runes after the match>[…]
```

- `…` (U+2026) marks a side that was cut. A side that reaches the start or end of the line gets no marker.
- The match itself is never cut, even when it is longer than `maxLineRunes`. The match is what the user reviews and signs, so it must appear whole.
- Lines of `maxLineRunes` runes or fewer print unchanged, so ordinary findings look exactly as they do today.

Where the window is anchored:

- **Single-line match (`!strings.Contains(i.Match, "\n")`).** Each body line from `bodyLines` is windowed around the **first** occurrence of `i.Match` in that line. Other occurrences that fall inside the window are still highlighted by the existing `strings.ReplaceAll` (`command/report.go:63`); occurrences outside it are dropped with the cut-off text.
- **Multi-line match.** `bodyLines` returns the whole chunk (`command/report.go:117-118`). Find the match once in `strings.Join(i.Chunk.Lines, "\n")` with `strings.Index`, and map that byte span onto the lines it covers.
  - The match's first segment is a suffix of its line and its last segment is a prefix of its line, so a long line holding the first or last segment keeps the segment plus `contextRunes` of context on its open side only.
  - Lines strictly inside the match are entirely match, so they are never cut.
  - The windowed lines therefore still contain `i.Match` contiguously across `\n`, so the existing joined `ReplaceAll` and the per-segment `highlight` (`command/report.go:100`) keep working unchanged.
  - A long chunk line outside the match span is windowed around an empty anchor at its start, so it keeps its first `contextRunes` runes followed by `…`.
  - Only the first occurrence of a multi-line match is anchored. Today `ReplaceAll` highlights every occurrence in the chunk; after the change, a later occurrence on a long line may be cut and lose its highlight. The finding is still reported and signed by its `Match`, so this only affects display.

Apply windowing to the plain lines **before** highlighting, so that ANSI escape sequences are never counted or cut.

Put the rune-window computation in one small pure helper in `command/report.go`. For example: `excerpt(line string, start, end int) string`. It takes the byte span `[start, end)` of the anchor within `line`, as `strings.Index` returns it, and returns `line` unchanged when it has `maxLineRunes` runes or fewer, or else the windowed text with markers. Every case above is one call:

| Body line | Anchor span |
|---|---|
| single-line match | first occurrence of `i.Match` in the line |
| line holding the match's first segment | segment start to end of line |
| line holding the match's last segment | start of line to segment end |
| line strictly inside the match | whole line (never cut) |
| chunk line outside the match span | `[0, 0)` |

That keeps `issue` readable, and the helper can be tested in a table without building `detect.Issue` values.

### 2. Update the `issue` doc comment

The comment on `reporter.issue` (`command/report.go:41-43`) currently says the body is "only the added lines that contain the match". Extend it: a line longer than `maxLineRunes` runes is shortened to `contextRunes` runes on each side of the match (or to its first `contextRunes` runes when it holds none of the match), with `…` marking each cut.

### 3. Tests

- **`excerpt` (or whatever the helper is named).** Add a table-driven test in `command/report_test.go` covering:
  - a line exactly `maxLineRunes` long (unchanged);
  - an empty anchor at the start of a long line (first `contextRunes` runes, then `…`);
  - match at the start, in the middle and at the end of a long line (marker on one or both sides);
  - a match longer than `maxLineRunes` (kept whole);
  - multi-byte runes on both sides of the cut, to show the counting is per rune and the output is valid UTF-8.
- **Reporter fixtures.** Add cases to `TestReporterIssue` (`command/report_test.go:12`) with expected output in new files under `command/testdata/report/`:
  - `long.txt`: a single-line match in a long line, with a second occurrence outside the window and a third inside it;
  - `long-multiline.txt`: a multi-line match whose first and last lines are long, plus a long chunk line outside the match.

  Build the long inputs with `strings.Repeat` of a filler in the test. The expected files hold the windowed output, which is short.
- **Color.** Add one case to `TestReporterColor` (`command/report_test.go:75`) with a long line. It checks that `…` markers sit outside the escape sequences and only the match sits between `\x1b[93m` and `\x1b[0m`.

---

## What does NOT change

- Findings are not grouped. There is still one `~~~` block per finding, in the same order, with the same `Reason`/`Detail`/`Filepath`/`Signature` lines and commit metadata.
- Which lines are selected for the body (`bodyLines`'s selection rule), and the `+` prefix on each line.
- `detect.Issue` and `detect.Chunk` (`detect/detect.go:10-34`), including `Issue.Match` and the signature computation. Truncation only affects display.
- Config: no new `tartufo.toml` key, since the config keys are part of the compatibility contract. Both limits are constants.
- `pre-commit` (`command/precommit.go:70`) and `scan` (`command/scan.go:93`) both call `reporter.issue`, so both pick up the change. No call site changes.
- The existing fixtures in `command/testdata/` have no line over 200 characters, so their expected output stays byte-identical.

---

## Acceptance Criteria

- [ ] A body line of `maxLineRunes` runes or fewer prints exactly as before. All existing `TestReporterIssue` and `TestReporterColor` cases, and the `command/testdata/scan` and `command/testdata/precommit` end-to-end fixtures, pass unchanged.
- [ ] A body line longer than `maxLineRunes` prints as at most `contextRunes` runes, the match and at most `contextRunes` runes. There is a `…` on every side where text was dropped and none on a side that reaches the line's edge.
- [ ] A match longer than `maxLineRunes` runes appears whole in the output.
- [ ] Cuts fall on rune boundaries: the output for a line of multi-byte characters is valid UTF-8, and context lengths match `contextRunes` counted in runes.
- [ ] For a multi-line match spanning long lines, the colored output highlights every segment of the match, and the `…` markers fall outside the escape sequences.
- [ ] After `make build`, `bin/gelato --no-color scan /Users/kxue/projects/jarvis-registry-workspace/jarvis-registry` (run from the gelato root) still reports 510 issues with the same set of signatures. The report shrinks from about 92 MB to under 2 MB, and no line is longer than `2*contextRunes` runes plus the match plus the `+` prefix and markers.
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `command/report.go` | Add `maxLineRunes`/`contextRunes` constants and a rune-based window helper, window long body lines before highlighting, update the `issue` doc comment |
| `command/report_test.go` | Table test for the window helper; new `TestReporterIssue` cases and a long-line `TestReporterColor` case |
| `command/testdata/report/long.txt` | Expected output for a long single-line match |
| `command/testdata/report/long-multiline.txt` | Expected output for a multi-line match over long lines |

---

## Risk

The likeliest regression is multi-line highlighting: if a window cuts into the match's span across `\n`, the joined `ReplaceAll` stops matching and the highlight silently disappears. The long-line `TestReporterColor` case and the `long-multiline.txt` fixture guard against this, and the existing short-line fixtures guard against changes to ordinary output.

---

## Out of scope / follow-up

- In jarvis-registry, add `search/search_index\.json$` to `exclude-path-patterns`. It's MkDocs build output on `origin/gh-pages`, and its sources under `docs/` are scanned on `main`.
- Grouping findings per file or per line was considered and rejected. The matches on a long line are far apart, so grouping would still print about 1 MB per line, and it would change the report layout tartufo users know.
