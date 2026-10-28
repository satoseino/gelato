# Add `--output-format=json` and an agent-readable finding guide to `--help`

## Background

AI coding agents are now often asked to run a secret scanner and fix its false positives. gelato's only output is the text report from `reporter.issue` (`command/report.go:56`). It prints `Reason`, `Detail`, `Filepath`, `Signature`, commit fields and the body lines, but never the flagged string as its own field: the match is shown only by a yellow highlight. `useColor` (`command/command.go:25`) turns color off whenever stdout isn't a terminal, and an agent's stdout never is. So an agent sees `+a = "…" b = "…"` and has to guess which token was flagged. tartufo already has `--output-format json`, with keys `file_path`, `matched_string`, `signature`, `issue_type`, `issue_detail` and `diff` plus flattened commit metadata, under a top-level `found_issues` key (`tartufo/tartufo/scanner.py:85-95`, `tartufo/tartufo/util.py:145-178`). Decision: add the same flag and the same key names to gelato. Also give `scan --help` and `pre-commit --help` a guide that tells an agent how to read a finding and how to resolve it.

---

## Changes

### 1. `--output-format` global flag

In `command/command.go`, add to `Globals` (`command/command.go:13`):

```go
OutputFormat string `default:"text" enum:"text,json" help:"..." name:"output-format"`
```

Add the unexported constant `formatJSON = "json"`; `newReporter` picks text for any other value, so no `formatText` is needed. Help text: `Report format: "text" for people, or "json" for scripts and AI agents (exactly one JSON document on stdout). See "gelato <command> --help" for the JSON fields and how to resolve a finding.`

- It is a global flag so that `pre-commit` and `scan` both get it.
- The `"` characters in the help text are escaped as `\"` inside the struct tag. A probe build with kong v1.16.1 rendered them correctly.
- `enum` makes kong reject any other value at parse time, so it ends in exit code 2 with `gelato: error: --output-format must be one of "text","json" but got "xml"` on stderr (probe output).
- Use tartufo's flag name. Don't add tartufo's `-of` short flag, because kong short flags are a single rune. Don't add `compact` or `report` either: JSON plus `jq` covers them.
- `--no-color` and `NO_COLOR` have no effect on JSON output.

### 2. Split the reporter into a consumer interface with text and JSON implementations

The two commands hold a concrete `reporter` value (`command/precommit.go:16`, `command/scan.go:19`) and call `issue` once per finding, then `summary` (`command/precommit.go:70-77`, `command/scan.go:92-100`). JSON output needs to collect every issue and write one document at the end, so in `command/report.go`:

- Rename the existing struct `reporter` to `textReporter`. Its output stays byte-identical.
- Define an unexported interface `reporter` with `issue(i detect.Issue, commit *git.CommitInfo) error` and `summary(n int) error`. The commands keep their `out reporter` field; only `scan`'s `issue` call site changes (see below). The interface lives in the consumer package, as CLAUDE.md requires.
- `newReporter(g, w)` (`command/report.go:46`) returns `&jsonReporter{w: w}` when `g.OutputFormat == formatJSON`, and `textReporter{w: w, color: useColor(g, w)}` otherwise.

**Replace `meta []field` with `commit *git.CommitInfo`.** Today's `field` (`command/report.go:20`) holds display labels such as `"Commit hash"`. JSON needs stable keys instead, and the only metadata gelato ever passes is the three fields of `git.CommitInfo` (`git/history.go:26-34`). So:

- `issue` takes `*git.CommitInfo`, and `nil` means no commit, which is what `pre-commit` passes.
- `textReporter.issue` prints `Commit hash: <Hash>`, `Commit time: <Time>` and `Commit message: <Subject>`, in that order, when `commit != nil`. That is exactly what `commitMeta` (`command/scan.go:242-246`) builds today, so the text report doesn't change.
- Remove `field`. In `command/scan.go`, `commitMeta` (`command/scan.go:223`) returns `map[int]git.CommitInfo`, keyed by rev-list index as now. `Run` passes `&info` for each finding.

### 3. `jsonReporter`

Add `jsonReporter` to `command/report.go`. It has pointer-receiver methods and holds `w io.Writer` and `issues []jsonIssue`.

```go
jsonIssue struct {
    FilePath      string           `json:"file_path"`
    MatchedString string           `json:"matched_string"`
    Signature     string           `json:"signature"`
    IssueType     detect.IssueType `json:"issue_type"`
    IssueDetail   *string          `json:"issue_detail"`
    Lines         []string         `json:"lines"`
    *jsonCommit
}

jsonCommit struct {
    CommitHash    string `json:"commit_hash"`
    CommitTime    string `json:"commit_time"`
    CommitMessage string `json:"commit_message"`
}
```

- `issue` appends one `jsonIssue`. Key order follows tartufo, which `encoding/json` keeps in struct field order.
  - `IssueDetail` is `nil` when `i.Detail == ""`, which is an entropy issue. That emits `null`, as tartufo's `None` does.
  - `Lines` is `excerpts(i)` (`command/report.go:147`): the same body lines as the text report, already cut to a window by spec 008, with no `+` prefix and no highlighting. It replaces tartufo's `diff`, which is the whole chunk and would bring back the 92 MB report spec 008 fixed. It uses a new name because its content differs from `diff`.
  - `jsonCommit` is embedded through a pointer. When `commit == nil` the three `commit_*` keys are left out entirely. When it is set, they are always present, even for an empty commit subject. tartufo's `branch` key is dropped because gelato doesn't track branches.
- `summary(_ int)` writes `{"found_issues": [...]}` with a `json.Encoder` on `w`. It substitutes `[]jsonIssue{}` when `issues` is nil, because a nil slice marshals as `null` and the key must always hold an array, however the reporter was constructed. It uses `SetEscapeHTML(false)`, so that `<`, `>` and `&` in `matched_string` appear literally and can be copied as they are, and `SetIndent("", "  ")`. The document ends with the encoder's trailing newline, so the empty result is exactly `{\n  "found_issues": []\n}\n`. The `n` argument isn't used: the slice is the count.
- Leave out tartufo's `scan_time`, `project_path`, `output_dir` and `excluded_*` envelope keys. `scan_time` would make output nondeterministic (CLAUDE.md requires deterministic output), and the rest repeats configuration the agent can read from `tartufo.toml`. There is no "All clear" or "Found N issue(s)" line.

Resulting stdout contract: when the exit code is 0 or 1, stdout holds exactly one JSON document and nothing else. When it is 2, stdout is empty, because every exit-2 error other than a failed write to stdout happens before `summary`, and `exitCode` (`cmd/gelato/main.go:84`) writes the error to stderr. Issue order is the text report's order.

Example (`scan`):

```json
{
  "found_issues": [
    {
      "file_path": "config.py",
      "matched_string": "<key>",
      "signature": "<signature>",
      "issue_type": "Regular Expression",
      "issue_detail": "AWS access key ID",
      "lines": [
        "aws_access_key_id = <key>"
      ],
      "commit_hash": "<hash>",
      "commit_time": "2020-01-01T00:00:00Z",
      "commit_message": "add key"
    }
  ]
}
```

### 4. Finding guide in `pre-commit --help` and `scan --help`

kong v1.16.1 prints the `Help() string` of a command struct (`HelpProvider`, `kong/help.go:77`, set as `node.Detail` in `kong/build.go:267`) under that command's `--help`. It formats the text with `go/doc.ToText` (`kong/help.go:431`), so the text must follow godoc comment syntax: blank lines between paragraphs, `  - ` list items, and indented lines for verbatim blocks such as TOML.

Add `func (*PreCommit) Help() string` and `func (*Scan) Help() string`, each with a doc comment. Both return text built by one unexported helper in `command/command.go`, for example `findingHelp(scan bool) string`, so that the two can't drift apart. The text covers these points, in this order, and uses placeholders only, so the repository never holds a token-shaped string:

1. **Exit codes.** 0 when nothing is found, 1 when secrets are found, and 2 for any other error, which is printed on stderr.
2. **JSON output.** With `--output-format=json`, stdout is exactly one JSON document, `{"found_issues": [...]}`, even when nothing is found. On exit code 2, stdout is empty.
3. **One list item per key,** each defined in one line:
   - `file_path`: the repository-relative path the string was added to;
   - `matched_string`: the exact flagged string, which may span lines;
   - `signature`: the finding's ID for `exclude-signatures`, computed from `matched_string` and `file_path` only;
   - `issue_type`: `"Regular Expression"` or `"High Entropy"`;
   - `issue_detail`: the regex rule's name, or `null` for High Entropy;
   - `lines`: the added lines containing `matched_string` (every added line of the change when it spans lines). A line longer than 500 characters is cut to 80 characters on each side of the match, or to its first 80 characters when it holds none of the match, and `…` marks each cut.
   - **`scan` only:** `commit_hash`, `commit_time`, `commit_message`: the oldest commit that added the finding.
4. **Resolving a finding.** First decide whether `matched_string` is a real secret.
   - If it is, remove it from the file and rotate it at its issuer. The `scan` text adds that removing it doesn't remove it from history.
   - If it isn't, add **one** entry under `[tool.tartufo]` in `tartufo.toml` (or `pyproject.toml`), with a `reason`, in a verbatim block:
     - `exclude-signatures = [{signature = "<signature>", reason = "<why this is not a secret>"}]`: the default choice. It suppresses exactly this string in this file.
     - `exclude-path-patterns = [{path-pattern = '<regex>', reason = "..."}]`: for files that are entirely generated or test data. The regex is matched from the start of `file_path` (`cfg/config.go:295`), and every finding in a matching file is skipped (`detect/engine.go:85-89`).
     - `exclude-entropy-patterns = [{pattern = '<regex>', path-pattern = '<regex>', reason = "..."}]`: for High Entropy issues only. It suppresses every high-entropy string on a line that `pattern` matches anywhere, in files whose path `path-pattern` matches anywhere. Both are unanchored (`cfg/config.go:40`, `detect/engine.go:160-164`), and `path-pattern` is optional.
   - Prefer the narrowest entry. Never put `matched_string` itself in the config, because `exclude-signatures` also accepts the raw string and that would commit the secret.

In the guide, write each TOML example as an indented verbatim block, and each key as a `  - ` list item. A `go/doc.ToText` probe confirmed that it keeps indented blocks verbatim and wraps list items with a hanging indent.

In the root `gelato --help` (`cmd/gelato/testdata/help.txt`), the only content change is the new `--output-format` flag. The longer flag name widens the flag column, though, so every flag line reflows and the golden has to be regenerated rather than patched. The guide appears on both subcommands and not on the root: kong prints `Detail` only in a node's own help (`kong/help.go:183`).

### 5. Tests

- **`TestReporterIssue` / `TestReporterColor`** (`command/report_test.go:14,102`). Switch to `textReporter{...}` and `commit *git.CommitInfo`. Update the `meta` case (`command/report_test.go:48-56`) to a `git.CommitInfo`, and rewrite `command/testdata/report/meta.txt` to its `Commit hash`, `Commit time` and `Commit message` lines. Every other text fixture stays byte-identical.
- **`TestJSONReporter`** (new, `command/report_test.go`). A table of issue lists, each compared byte-for-byte with `command/testdata/report/<name>.json`:
  - `empty`: no issues, giving exactly the empty document from section 3;
  - `regex-commit`: a regex issue with a `CommitInfo`, showing all keys and their order;
  - `entropy`: `"issue_detail": null` and no `commit_*` keys;
  - `multiline`: `matched_string` containing `\n`;
  - `long`: `lines` windowed with `…`;
  - `html`: a match containing `<&>`, emitted unescaped.

  Each case also unmarshals the output into `map[string]any` (not into `jsonIssue`: since Go 1.10, `encoding/json` can't unmarshal into an embedded pointer to an unexported struct type) and checks that every `matched_string` equals its issue's `Match`.
- **End-to-end** (`command/scan_test.go`, `command/precommit_test.go`). Add `TestScanJSON` and `TestPreCommitJSON`. They build the same repositories as `TestScanAddedThenRemoved` (`command/scan_test.go:71`) and `TestPreCommitFindings` (`command/precommit_test.go:163`), run with `&Globals{OutputFormat: "json"}`, check `ErrFindings`, and compare against new templates `command/testdata/scan/added-removed.json.tmpl` and `command/testdata/precommit/findings.json.tmpl`. Add a clean `pre-commit` case expecting exactly `{\n  "found_issues": []\n}\n` and a nil error.
- **CLI** (`cmd/gelato/main_test.go`):
  - In `TestRun`, add args `--output-format=xml pre-commit` (exit 2, stderr exactly as in section 1, empty stdout), and the invalid-config case with `--output-format=json` (exit 2, empty stdout).
  - Add goldens `cmd/gelato/testdata/help-scan.txt` and `help-pre-commit.txt` for `scan --help` and `pre-commit --help`, and regenerate `help.txt`.
  - Add a `TestRunScan` variant with `--output-format=json` on a clean repository, checking that stdout parses as JSON with an empty `found_issues`.

---

## What does NOT change

- The text report is byte-identical: no `Match:` line, same layout, same summary line. All existing `command/testdata/report`, `scan` and `precommit` fixtures except `meta.txt` pass unchanged.
- Exit codes 0 / 1 / 2 and `ErrFindings` (`command/command.go:20`).
- `detect.Issue`, signatures, dedup, ordering, and the windowing in `excerpt`/`excerpts`.
- Config: no new `tartufo.toml` key.
- No line numbers in either format.
- `commit_message` holds the commit subject (`git.CommitInfo.Subject`), as the text report does. tartufo emits the full message.

---

## Acceptance Criteria

- [ ] `gelato scan --output-format=json <repo>` and `gelato pre-commit --output-format=json` write exactly one JSON document to stdout on exit codes 0 and 1. On a clean repository it is exactly `{\n  "found_issues": []\n}\n`.
- [ ] Each issue has keys `file_path`, `matched_string`, `signature`, `issue_type`, `issue_detail` and `lines`, in that order. `scan` issues also have `commit_hash`, `commit_time` and `commit_message`, and `pre-commit` issues have none of the three.
- [ ] For UTF-8 input, `matched_string` equals `detect.Issue.Match` byte for byte, including `\n` in multi-line matches and `<`, `>`, `&` unescaped.
- [ ] `issue_detail` is `null` for High Entropy issues and the rule name for regex issues.
- [ ] `lines` equals the text report's body lines without the `+` prefix, windowed as in spec 008.
- [ ] `--output-format=xml` exits 2 with an error on stderr and nothing on stdout. Any exit-2 run with `--output-format=json` leaves stdout empty.
- [ ] `gelato scan --help` and `gelato pre-commit --help` match their goldens and contain the exit codes, every JSON key, and the three exclusion entries with placeholders. Only `scan --help` lists the `commit_*` keys.
- [ ] Text output is unchanged: every existing text fixture other than `command/testdata/report/meta.txt` passes without edits.
- [ ] Two consecutive `--output-format=json` scans of the same repository produce identical stdout (extend `TestScanDeterministic`, `command/scan_test.go:258`, or cover it in `TestScanJSON`).
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `command/command.go` | Add `Globals.OutputFormat`, the `formatJSON` constant and the shared `findingHelp` text |
| `command/report.go` | `reporter` interface; rename the struct to `textReporter`; `issue` takes `*git.CommitInfo`; remove `field`; add `jsonReporter`, `jsonIssue`, `jsonCommit`; `newReporter` picks the format |
| `command/scan.go` | Add `Scan.Help()`; `commitMeta` returns `map[int]git.CommitInfo`; pass `&info` to `issue` |
| `command/precommit.go` | Add `PreCommit.Help()`; pass `nil` commit to `issue` |
| `command/report_test.go` | Switch to `textReporter`/`CommitInfo`; add `TestJSONReporter` |
| `command/testdata/report/meta.txt` | Commit lines from `CommitInfo` replace the `Branch` line |
| `command/testdata/report/{empty,regex-commit,entropy,multiline,long,html}.json` | JSON reporter goldens |
| `command/scan_test.go` | Add `TestScanJSON` and a JSON determinism check |
| `command/testdata/scan/added-removed.json.tmpl` | JSON golden for `scan` |
| `command/precommit_test.go` | Add `TestPreCommitJSON`, with clean and findings cases |
| `command/testdata/precommit/findings.json.tmpl` | JSON golden for `pre-commit` |
| `cmd/gelato/main_test.go` | Invalid-enum and JSON exit-2 cases, subcommand help goldens, a JSON `TestRunScan` variant |
| `cmd/gelato/testdata/help.txt` | Regenerate: the new `--output-format` flag widens the flag column |
| `cmd/gelato/testdata/help-scan.txt`, `help-pre-commit.txt` | New goldens for subcommand help |

---

## Risk

The likeliest regression is in the text report, from moving `meta []field` to `*git.CommitInfo` and renaming the struct. The unchanged `command/testdata/scan` and `precommit` text fixtures, and the other `report/*.txt` fixtures, guard against it. The help goldens guard the guide against kong/`go/doc` reflowing its verbatim TOML blocks.

---

## Out of scope / follow-up

- `encoding/json` replaces invalid UTF-8 with U+FFFD. For a non-UTF-8 added line, `matched_string` would then differ from the file's bytes, although `signature` is still computed on the raw bytes, so `exclude-signatures` keeps working.
- Line numbers in findings would need hunk positions carried through `git.FileChange` and `detect.Chunk`. That needs its own spec.
- `README.md` doesn't exist yet. When spec 007 adds it, its Usage section should mention `--output-format`.
