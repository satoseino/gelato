# Implement `gelato pre-commit`: scan staged additions and report findings

## Background

After specs 1–4, `command.PreCommit` (`command/precommit.go`) loads config and stops at `Run() → "pre-commit: not implemented"`. The pieces it needs exist: `cfg.Config` (spec 1), `detect.Engine` and `detect.DefaultRules` (specs 2, 3), and `git.Repo.Staged` (spec 4). Tartufo's equivalent is `tartufo/tartufo/commands/pre_commit.py` driving `GitPreCommitScanner` (`tartufo/tartufo/scanner.py:965-991`), with text output from `Issue.__str__` (`scanner.py:102-121`), colors from `util.init_styles` (`tartufo/tartufo/util.py:243-259`) and exit codes from `cli.py:327-335`. This spec wires them together, adds the shared text reporter that spec 6 reuses, and adds `--no-color`.

---

## Changes

### 1. `--no-color` and writer injection

- Add `NoColor bool` to `command.Globals` (`name:"no-color" help:"Disable colored output."`).
- In `cmd/gelato/main.go`'s `run`, bind the `stdout` writer with `kong.BindTo(stdout, (*io.Writer)(nil))`, so command hooks receive the same writer that `run`'s tests capture.
- Color is on only when `NoColor` is false, the `NO_COLOR` environment variable is unset or empty, and the writer is an `*os.File` that is a terminal (`golang.org/x/term.IsTerminal`, pure Go). This mirrors tartufo's TTY default (`util.py:244`) plus the common `NO_COLOR` convention.

### 2. Shared reporter — `command/report.go`

`type reporter struct { w io.Writer; color bool }` with `func (r reporter) issue(i detect.Issue, meta []field)`, where `field` is a label/value pair (empty for pre-commit; commit details in spec 6). It prints tartufo's layout:

```
~~~~~~~~~~~~~~~~~~~~~
Reason: High Entropy
Detail: <rule name>          (regex issues only)
Filepath: <path>
Signature: <signature>
<label>: <value>             (one per meta field)
+<added line containing the match>
~~~~~~~~~~~~~~~~~~~~~
```

- The `Reason`, `Detail`, `Filepath`, `Signature` and meta lines are bright green, and the match inside the body is bright yellow, when color is on (tartufo's `style_ok` and `style_warning`).
- **Body divergence from tartufo:** tartufo prints the whole diff chunk (`scanner.py:104`), which for a newly added file is the entire file. gelato prints only the added lines containing the match, each prefixed with `+`. If the match spans lines (a regex using `\s`), all of the chunk's lines are printed.
- After all issues: `Found <n> issue(s).` When there are none: `All clear. No secrets detected.` (tartufo's message, `util.py:192`, without the timestamp).

### 3. `command.PreCommit.Run` — `command/precommit.go`

1. `git.Open(".")` with the default inherited environment, so a temporary index from `git commit -a` / `git commit <paths>` is what gets scanned. Staged renames are detected (`-M50%`, spec 4), so only lines added by the rename's edit are scanned. Tartufo's pre-commit diff doesn't run rename detection (`scanner.py:984-991` has no `find_similar`), so it scans a renamed file in full; this is listed in the README (spec 7).
2. `detect.New(c.cfg, detect.DefaultRules())`.
3. Turn each `git.FileChange` into chunks:
   - one chunk `{Path, Lines}` when it has added lines;
   - for `Added` and `Renamed` changes, one extra chunk `{Path: p, Lines: []string{p}}`, so secrets in file names are caught. It is a separate chunk so its output body is just the path.
4. Fan the chunks out to `runtime.GOMAXPROCS(0)` worker goroutines calling `Engine.Scan` (goroutine-safe per spec 2). Each chunk carries its sequence number, and results are reassembled in that order. Output is therefore in git's path order and identical between runs.
5. Report through the reporter. If any issue was found, return `command.ErrFindings` (exit 1, spec 1); otherwise return nil (exit 0). Git and config errors propagate (exit 2).

### 4. Tests — `command/precommit_test.go`

End-to-end, using `internal/gittest` (spec 4) and fragment-built secrets (spec 3):

- A clean staged file: exit 0 and the "All clear" message.
- A staged regex hit and a staged entropy hit: exit 1, both in the output with tartufo's layout, `Found 2 issue(s).`, no ANSI codes when the writer is a buffer.
- The same hit excluded by `exclude-signatures` (both raw string and computed signature) and by `exclude-path-patterns`: exit 0.
- A secret-shaped file name in a newly added file: reported with the path as body.
- Only added lines count: a secret on an unchanged or removed line is not reported.
- An unborn branch and a staged rename.
- Deterministic output: two runs over a repository with many staged files produce byte-identical output.
- **Real hook:** the test builds the binary into its own `t.TempDir()` (not in `TestMain`, so a failed build fails only this test), installs a `.git/hooks/pre-commit` that runs `gelato pre-commit`, then runs `git commit -a` (temporary `GIT_INDEX_FILE`) with a modified tracked file containing a secret, and asserts the commit is rejected and the finding is printed.
- Color: a forced-color reporter test checks the escape sequences around the match.

Add the scoped `.golangci.yaml` exclusion for `G204` on `command/precommit_test.go` (building and running the gelato binary for the real-hook test), with a comment explaining why, as in spec 4.

---

## What does NOT change

- No JSON, compact or report output; no `--output-dir`, `--quiet` or `--verbose`.
- No history scanning (spec 6) and no `.pre-commit-hooks.yaml` (spec 7).
- `detect`, `cfg` and `git` package behavior; this spec only composes them.

---

## Acceptance Criteria

- [ ] Staging a file whose added line holds a fragment-built AWS access key ID makes `gelato pre-commit` exit 1 and print `Reason: Regular Expression`, `Detail: AWS access key ID`, the path, and the signature.
- [ ] Adding that signature to `exclude-signatures` in `tartufo.toml` makes the same run exit 0.
- [ ] A secret only on a removed line exits 0.
- [ ] `git commit -a` through an installed hook is rejected when the working-tree change adds a secret.
- [ ] Output contains no ANSI escapes when piped, with `--no-color`, or with `NO_COLOR=1`.
- [ ] Two runs over the same staged state produce byte-identical stdout.
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `go.mod`, `go.sum` | Add `golang.org/x/term` |
| `cmd/gelato/main.go` | Bind stdout as `io.Writer` |
| `command/command.go` | `Globals.NoColor`; color decision helper |
| `command/report.go` | Shared text reporter |
| `command/report_test.go` | Layout, body selection and color tests |
| `command/precommit.go` | Real `Run`: git, engine, worker pool, reporting, exit signaling |
| `command/precommit_test.go` | End-to-end and real-hook tests |
| `.golangci.yaml` | Scoped `G204` exclusion for `command/precommit_test.go` |

---

## Risk

The real-hook test is the only check that the hook sees the temporary index git creates for `git commit -a`; if it is flaky or skipped, the main failure mode (scanning the wrong snapshot) goes unguarded.
