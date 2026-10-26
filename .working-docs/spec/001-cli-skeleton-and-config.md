# Add the CLI skeleton and tartufo-compatible config loading

## Background

gelato has only scaffolding so far (`go.mod`, `Makefile`, `.golangci.yaml`, `CLAUDE.md`); `Makefile`'s `build` target already points at a not-yet-existing `./cmd/gelato`. Every later spec needs a kong entry point with a fixed exit-code contract and a loader that reads existing users' `tartufo.toml` unchanged, so both land first. Tartufo's config behavior lives in `tartufo/tartufo/config.py` (discovery `:34-69`, `--config` merging `:72-123`, pattern compilation `:230-284`) and is consumed in `tartufo/tartufo/scanner.py` (`:270-435`); this spec re-implements the subset the team uses (the six keys in `jarvis-registry/tartufo.toml`), without copying tartufo code.

---

## Changes

### 1. Add dependencies

Add `github.com/alecthomas/kong v1.16.1` (CLI) and `github.com/pelletier/go-toml/v2 v2.4.3` (TOML; MIT, maintained, pure Go) to `go.mod`/`go.sum`.

### 2. `cmd/gelato/main.go` — root command and exit-code contract

- Root CLI struct (field order = help order):
  - `PreCommit command.PreCommit` — `cmd:"" name:"pre-commit" help:"Scan lines added in staged changes."`
  - `Scan command.Scan` — `cmd:"" name:"scan" help:"Scan lines added across the repository's history."`
  - embedded `command.Globals`
  - `Version kong.VersionFlag` — `short:"V"` (tartufo's flag, `tartufo/tartufo/cli.py:272`).
- `var version = "dev"`, overridden at release time with `-ldflags "-X main.version=..."` (wired in spec 7).
- `main()` calls `os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))`. `run` builds the parser with `kong.New` (not `kong.Parse`), wires `kong.Writers(stdout, stderr)` and `kong.Exit(...)` so `--help`/`--version` return from `run` instead of exiting the process, then parses and runs.
- Exit codes are decided by `run`, never by kong's `FatalIfErrorf` (kong uses 80 for parse errors and for errors returned from `AfterApply`, see `kong@v1.16.0/kong.go:326-350`):
  - `0` — success, including `--help`/`--version`.
  - `1` — the command returned an error matching `command.ErrFindings` (`errors.Is`). Mirrors tartufo's `process_exit` (`tartufo/tartufo/cli.py:327-335`).
  - `2` — any other error, including kong parse errors and config errors. The message is printed to stderr as `gelato: error: <message>`.

### 3. `command` package — globals, sentinel, command stubs

New package `command` (owns the kong commands; real behavior arrives in specs 5 and 6):

- `command.Globals` — `Config []string` with `name:"config" type:"path" help:"Read configuration from this file. Repeatable; list values are concatenated in order."` Bound into kong with `kong.Bind(&cli.Globals)` so command hooks can receive it.
- `command.ErrFindings` — exported sentinel, `errors.New("secrets found")`, the only way a command signals exit code 1.
- `command.PreCommit` — `AfterApply(g *Globals) error` calls `cfg.Load(g.Config, ".")` and stores the result on an unexported field; `Run() error` returns `errors.New("pre-commit: not implemented")`. The repository dir is the current directory, as tartufo's `pre-commit` assumes (`tartufo/tartufo/commands/pre_commit.py`, `repo_path = pathlib.Path.cwd()`).
- `command.Scan` — `RepoPath string` positional, `arg:"" optional:"" name:"repo-path"`. `AfterApply(g *Globals)` calls `cfg.Load(g.Config, c.RepoPath)`; `Run()` returns `errors.New("scan: not implemented")`.

### 4. `cfg` package — discovery, merging, validation

`func Load(explicit []string, repoDir string) (Config, error)`:

1. **Explicit files** (`--config`, resolved against the working directory): each must exist and contain a non-empty `[tool.tartufo]` table, else error (tartufo treats a missing or empty table as "no config", `config.py:63-69`, and `--config` turns that into an error, `:96-100`). An empty table is reported the same way as a missing one.
2. **Repository dir**: if `repoDir == ""` (only `scan` without an argument), use `repo-path` from the merged explicit files, else `"."`. This matches tartufo's `scan-local-repo` taking its `repo-path` argument default from `--config` (`commands/scan_local_repo.py:19-20`). The result is stored as `Config.RepoPath`, resolved to an absolute path. `repo-path` from the auto-discovered target file is validated but has no effect.
3. **Target file**: in the repository dir, the first of `tartufo.toml`, `pyproject.toml` that exists. If that file has no `[tool.tartufo]` table, or an empty one, there is no target config and the other file is not consulted, as in tartufo (`config.py:57-68` stops at the first existing file). Skipped if it resolves to the same file as an explicit one (tartufo's `REFERENCED_CONFIG_FILES` check, `scanner.py:178`). No file found is not an error.
4. **Merge** in order target file, then explicit files: list values concatenate, scalars from later files override earlier ones (tartufo's list concatenation, `config.py:111-116`).

Two deliberate divergences from tartufo, both listed in the README (spec 7):

- Tartufo reads only list keys from the target file (`config_data.get(...)` in `scanner.py:270-410`) and takes `regex`/`entropy` only from `--config` files and flags. gelato also applies `regex`/`entropy` from the target file, which is what a user who wrote them there expects.
- Tartufo silently ignores a target file it can't parse (`scanner.py:171-175`). gelato fails with the parse error, per `CLAUDE.md` ("Config").

Validation (all errors name the file and key path, 0-based indices, e.g. `tartufo.toml: exclude-entropy-patterns[2].pattern: error parsing regexp: ...`):

- Key normalization: `_` and `-` are interchangeable (`repo_path` == `repo-path`), as in `config.py:69`.
- Allowed top-level keys: `repo-path` (string), `regex` (bool), `entropy` (bool), `exclude-path-patterns`, `exclude-entropy-patterns`, `exclude-signatures` (arrays of tables). Any other key, or a wrong type, is an error.
- `exclude-path-patterns[i]`: `path-pattern` (string, required), `reason` (string, optional). A bare string entry is an error (`scanner.py:302-323`).
- `exclude-entropy-patterns[i]`: `pattern` (required), `path-pattern` (optional, default `""`), `reason` (optional). `match-type` and `scope` are not supported (unused by the team's config) and are rejected as unknown keys.
- `exclude-signatures[i]`: `signature` (required), `reason` (optional). Duplicate signatures are allowed (the jarvis config contains one).
- `regex` and `entropy` default to `true`; both `false` is an error "no analysis requested" (`scanner.py:581-582`).

### 5. `cfg.Config` — compiled, Python-`re`-compatible patterns

```go
type Config struct {
	RepoPath               string
	Regex, Entropy         bool
	ExcludePaths           []*regexp.Regexp
	ExcludeEntropyPatterns []EntropyExclusion
	ExcludeSignatures      map[string]struct{}
}

type EntropyExclusion struct {
	Pattern, PathPattern *regexp.Regexp
	Reason               string
}
```

- Path patterns: whitespace-trimmed; empty or `#`-prefixed entries are skipped (`config.py:230-242`); compiled as `^(?:<p>)` because tartufo applies them with start-anchored `re.match` (`scanner.py:369`).
- Entropy patterns: `pattern` and `path-pattern` compiled unanchored, because tartufo's default `match-type` is `search` (`config.py:254`) and `rule_matches` uses `.search` for both (`scanner.py:434-464`). An empty `path-pattern` matches every path.
- A pattern that is valid Python but not RE2 (e.g. lookaround, `\Z`) fails `Load` with the key-qualified error above. No emulation.

### 6. Tests

- `cfg/config_test.go`: table-driven over `cfg/testdata/*.toml` fixtures — discovery order, `pyproject.toml` used only when `tartufo.toml` is absent, a `tartufo.toml` without the table meaning no target config, missing or empty table in an explicit file, explicit-file errors, merge order and list concatenation, explicit/target dedup, `repo-path` resolution, `_`/`-` normalization, every validation error message, `#`/blank path-pattern skipping, anchoring (`uv\.lock$` excludes `uv.lock` but not `sub/uv.lock`), entropy-pattern search semantics.
- `cfg/testdata/jarvis-like.toml`: a trimmed fixture with the same shapes as `jarvis-registry/tartufo.toml` (a few entries per key, including a duplicated signature).
- `cmd/gelato/main_test.go`: drives `run` in-process for `--help` (0), `--version` (0, prints `dev`), unknown flag (2), invalid `--config` file (2), invalid target config discovered by `pre-commit` in the working directory and by `scan <repo-path>` in its argument (2, error names `tartufo.toml` / `repo/tartufo.toml`), stub `Run` (2), and a test-only command returning `command.ErrFindings`-wrapped error mapped to 1 via the same mapping function.

---

## What does NOT change

- No scanning, git access, or output formatting (specs 2, 4, 5, 6).
- No `--no-color`, `--verbose`, `--quiet`, entropy-sensitivity, or any other tartufo flag; only `--config` and `--version`.
- No `gelato.toml` / `[tool.gelato]` support.

---

## Acceptance Criteria

- [ ] `make build` produces `bin/gelato` with `CGO_ENABLED=0`; `bin/gelato --help` lists `pre-commit` and `scan` and exits 0.
- [ ] `bin/gelato --version` and `-V` print `dev` and exit 0.
- [ ] Running `bin/gelato pre-commit` in `jarvis-registry/` loads its `tartufo.toml` without error and exits 2 with `gelato: error: pre-commit: not implemented`.
- [ ] Unknown key, wrong type, missing required sub-key, and invalid regex each exit 2 with an error naming file and key path.
- [ ] `regex = false` plus `entropy = false` exits 2 with "no analysis requested".
- [ ] `cfg.Load` with an explicit file and a different target file concatenates their `exclude-signatures`, and explicit scalars override target scalars.
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `go.mod`, `go.sum` | Add kong and go-toml/v2 |
| `cmd/gelato/main.go` | Root CLI, `run`, exit-code mapping, `version` var |
| `cmd/gelato/main_test.go` | In-process exit-code tests |
| `command/command.go` | `Globals`, `ErrFindings` |
| `command/precommit.go` | `PreCommit` stub with config loading |
| `command/scan.go` | `Scan` stub with `repo-path` arg and config loading |
| `cfg/config.go` | `Load`, `Config`, `EntropyExclusion`, validation and compilation |
| `cfg/config_test.go` | Table-driven loader tests |
| `cfg/testdata/*.toml` | Fixtures |

---

## Risk

Mistranslating tartufo's `match` vs `search` anchoring would silently exclude too much or too little later; the anchoring cases in `cfg/config_test.go` pin it.
