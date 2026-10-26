---
description: >
  Front matter tracks which findings have been posted to GitHub as change requests,
  which have been dismissed, and remaining concerns if any.
  changes_requested is a flow sequence of finding labels posted to GitHub that are still open.
  findings_dismissed is a flow sequence of finding labels the reviewer chose not to post.
  merged is a boolean that tracks if the PR has been merged.
  notes is for optional notes about remaining concerns.
pr_url: https://github.com/satoseino/gelato/pull/1
changes_requested: [m1, m2, m3]
findings_dismissed: []
merged:
notes:
---
# PR Review: Feature: CLI skeleton and tartufo-compatible config loading

**PR:** https://github.com/satoseino/gelato/pull/1
**Spec:** .working-docs/spec/001-cli-skeleton-and-config.md
**Reviewed:** 2026-10-26

## Summary

The PR implements spec 001 faithfully: the kong root with a panic/recover-based `kong.Exit` hook that keeps `--help`/`--version` in-process, a 0/1/2 exit-code mapping independent of kong's 80, and a strict `cfg.Load` that matches tartufo's discovery, dedup, merge, and `match`/`search` anchoring. `make all` passes. Manual checks also pass: `pre-commit --help` and `-V` still exit 0 when the cwd's `tartufo.toml` is invalid (the hooks run before `AfterApply`), `sep:"none"` keeps commas inside a `--config` path, and running with no args exits 2. The only findings are minor: two test-coverage gaps, one tartufo divergence the spec doesn't list, and a little redundant code.

## Spec Compliance

| Requirement | Status |
|---|---|
| 1. kong v1.16.1 + go-toml/v2 v2.4.3 deps | Met |
| 2. Root CLI (field order, `-V`, `version` var, `kong.New` + `Writers` + `Exit`, exit codes 0/1/2, `gelato: error:` prefix) | Met |
| 3. `command.Globals` / `ErrFindings` / `PreCommit` / `Scan` stubs, `kong.Bind(&Globals)` | Met |
| 4. Discovery order, first-existing-file stop, explicit/target `SameFile` dedup, `repo-path` resolution, merge order | Met |
| 4. Validation: `_`/`-` normalization, allowed keys/types, sub-key rules, `match-type`/`scope` rejected, duplicate signatures allowed, "no analysis requested" | Met (`_` normalization limited to top-level keys, matching tartufo `config.py:69`) |
| 5. Path patterns trimmed, `#`/blank skipped, `^(?:p)`; entropy patterns unanchored, empty `path-pattern` matches all; RE2-only failures key-qualified | Met |
| 6. Table-driven + golden-file `cfg` tests, `jarvis-like.toml`, in-process `run` tests incl. `ErrFindings` → 1 | Met (see [m1]) |
| AC: `make build`/`--help`/`--version`/`-V`/invalid-config exits | Met (verified locally) |
| AC: `pre-commit` in `jarvis-registry/` | Not verified in this review (PR description says it loads cleanly) |
| AC: `make all` passes | Met |

## Findings

### Critical

_None._

### Major

_None._

### Minor

**[m1] CLI tests don't exercise the `scan <repo-path>` argument or target-file discovery through a command** — `cmd/gelato/main_test.go` runs `scan` with no argument in an empty temp dir, and its only config case passes `--config`. So no CLI-level test would fail if `Scan.AfterApply` stopped passing `c.RepoPath` to `cfg.Load` (e.g. passed `"."` instead), or if `PreCommit` stopped discovering `./tartufo.toml`. AC3 (`pre-commit` loads the cwd's `tartufo.toml`) is only checked by hand. Two cases would pin this: `scan repo` where `repo/tartufo.toml` is invalid (expect exit 2 with `repo/tartufo.toml: ...`), and `pre-commit` with an invalid `tartufo.toml` in the cwd.

**[m2] An empty `[tool.tartufo]` table in a `--config` file is accepted, but tartufo rejects it, and this divergence isn't listed** — tartufo's `load_config_from_path` raises `FileNotFoundError` when the table is empty (`if not config:`, `tartufo/tartufo/config.py:66-67`), and `--config` turns that into an error. gelato's `readFile` returns `found=true` for an empty table, so `Load` accepts it. Being more lenient here does no harm, but the spec says gelato's tartufo divergences are listed in the README (spec 7), and this one isn't among them. Spec §4 (steps 1 and 3) and §6 have been updated to match tartufo: an empty table counts as "no table", so it is an error in an explicit file and means no target config. `readFile` should return `found=false` for an empty table, and a test should pin the explicit-file error.

**[m3] Small redundancies in `cfg/config.go`** — `asTables` already checks that `reason` is a string (`optionalString(at, table, keyReason)`), and `parseEntropyPatterns` checks it again when it reads `Reason`. `readFile` also calls `os.ReadFile` and then `os.Stat` on the same path; opening the file once and using `f.Stat()` would make both reads see the same file.

## Positive Observations

- The exit-code contract is decided entirely in `exitCode`. `TestExecuteFindings` reuses the same `execute` path with a test-only root, as the spec asked, instead of adding a production seam.
- Tartufo behavior is ported precisely: `_`→`-` only on top-level keys (sub-keys verbatim, pinned by `path-patterns-underscore-subkey`), the first existing target file stops discovery even if it has no table, `os.SameFile` dedup equivalent to `REFERENCED_CONFIG_FILES`, `strip()` + `#` skipping only for path patterns, and `repo-path` taken from the last explicit file.
- Errors are strict and key-qualified. Path patterns are compiled bare first so the error quotes what the user wrote rather than the `^(?:…)` wrapper, and `repo_path` + `repo-path` together is rejected instead of one silently winning.
- `TestLoadInvalid` runs every golden error fixture both as an explicit file and as the target file, which covers both code paths for every validation message.
- The `sep:"none"` on `--config` (not in the spec) prevents kong from splitting paths that contain commas.
- The lint exclusions are scoped by `path` + `text` with explanatory comments, and the `fieldalignment` suppression uses the inline form `CLAUDE.md` prescribes.

## Recommendation

**Approve** — the implementation meets the spec and acceptance criteria, and the remaining findings are minor test-coverage and documentation nits.
