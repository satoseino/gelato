---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/8
changes_requested: []
findings_dismissed: []
merged:
notes: m1, m2 and m3 were fixed directly on the branch (73502c2, 4da6893, 703f87a) instead of being posted as GitHub change requests.
---
# PR Review: Feature: add --output-format=json and a finding guide to subcommand help

**PR:** https://github.com/satoseino/gelato/pull/8
**Spec:** .working-docs/spec/009-json-output.md
**Reviewed:** 2026-10-27

## Summary

The PR follows spec 009 closely. It adds a global `--output-format=text|json` flag, splits the reporter into a consumer-side interface with `textReporter` and `jsonReporter`, replaces `meta []field` with `*git.CommitInfo`, and adds a shared finding guide to `scan --help` and `pre-commit --help`. The text output stays byte-identical: only `meta.txt` changed. JSON key order, `null` detail, omitted `commit_*` keys and unescaped HTML are all pinned by goldens. `make all` passes. The findings are minor: a doc comment that wasn't reflowed, help wording that leaves out one truncation case, and an unused constant.

## Spec Compliance

| Requirement | Status |
|---|---|
| §1 `OutputFormat` global flag, `enum`, escaped help text, `formatText`/`formatJSON` | Met |
| §2 `reporter` interface in consumer, `textReporter` rename, `*git.CommitInfo`, `field` removed, `commitMeta` → `map[int]git.CommitInfo` | Met |
| §3 `jsonReporter`/`jsonIssue`/embedded `*jsonCommit`, nil→`[]`, `SetEscapeHTML(false)`, indent, no envelope keys | Met |
| §4 `Help()` on both commands via one `findingHelp(scan bool)`; order and content of guide; placeholders only | Met (see [m2] on one wording gap) |
| §5 Report tests (`TestJSONReporter` six goldens + generic unmarshal check) | Met |
| §5 E2E `TestScanJSON`, `TestPreCommitJSON` (clean + findings) | Met |
| §5 CLI: invalid enum, invalid config with JSON, help goldens, `TestRunScanJSON` | Met |
| AC: one JSON document on exit 0/1; exact empty document | Met |
| AC: key set/order; `commit_*` only on `scan` | Met |
| AC: `matched_string` == `Match` incl. `\n` and `<&>` | Met |
| AC: `issue_detail` null for entropy | Met |
| AC: `lines` == text body without `+`, windowed | Met |
| AC: `--output-format=xml` exit 2, empty stdout; JSON exit-2 empty stdout | Met |
| AC: help goldens; only `scan --help` lists `commit_*` | Met |
| AC: text fixtures unchanged except `meta.txt` | Met |
| AC: JSON determinism across runs | Met (in `TestScanJSON`) |
| AC: `make all` passes | Met (verified locally) |

## Findings

### Critical

None.

### Major

None.

### Minor

**[m1] `textReporter.issue` doc comment not reflowed** — `command/report.go:91`. The edited first line, `// issue prints i followed by commit's metadata, if any. Unlike tartufo, which prints the whole`, is 95 characters. The lines after it still wrap at about 78, as before the edit, so the paragraph has a ragged first line. **Fixed in 73502c2:** the paragraph is reflowed.

**[m2] Finding guide's `lines` description leaves out the no-match truncation case** — `command/command.go` (`findingHelp`), and the goldens `cmd/gelato/testdata/help-scan.txt` and `help-pre-commit.txt`. The guide says a long line "is cut to 80 characters on each side of the match". For a multi-line match, `lines` holds every added line of the chunk, and a long line that holds none of the match is cut to its first 80 characters instead (`textReporter.issue`'s doc comment, `command/report.go:94-96`). An agent reading the guide may assume every truncated line contains the match. The spec's own wording has the same gap. **Fixed in 4da6893:** the guide adds "or to its first 80 characters when it holds none of the match", both goldens are regenerated, and the spec's wording is updated to match.

**[m3] `formatText` constant is declared but never used** — `command/command.go:22`. Only `formatJSON` is referenced (`command/report.go:84`); the flag's `default:"text" enum:"text,json"` tag has to repeat the literal anyway. `newReporter` treats any value that isn't `"json"` as text, so `formatText` is dead code. **Fixed in 703f87a:** `formatText` is dropped, `newReporter`'s doc comment now says that any value other than `formatJSON` selects text, and spec §1 and the Files table are updated.

## Positive Observations

- The text report stays byte-identical. Every `scan`/`precommit` text fixture and every `report/*.txt` fixture except `meta.txt` is unchanged, which guards the riskiest part of the refactor.
- `TestJSONReporter` goes through `newReporter`, so it also covers format selection. Its generic `map[string]any` decode checks `matched_string` round-trips and works around the embedded unexported pointer, as the spec describes.
- One `findingHelp(scan bool)` builds both guides, so they can't drift apart, and the help goldens pin how `go/doc` reflows the text, including the verbatim TOML blocks.
- The guide's exclusion semantics match the code: `exclude-path-patterns` is anchored with `^(?:…)` (`cfg/config.go:295`), and the entropy patterns use unanchored `MatchString` (`detect/engine.go:160`).
- The `fieldalignment` suppressions are inline and scoped to the linter, with reasons, as CLAUDE.md requires.
- Exit-2 paths leave stdout empty for both an enum error and a config error. `TestRun` asserts stdout on every case, so this is checked.

## Recommendation

**Approve** — the implementation meets every spec requirement and acceptance criterion, and the remaining findings are minor fixes to docs and tidiness.
