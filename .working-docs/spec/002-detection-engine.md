# Add the detection engine (regex, entropy, exclusions, signatures)

## Background

Spec 1 produces a validated `cfg.Config`, but nothing scans text yet. Tartufo's detection logic is in `tartufo/tartufo/scanner.py` (`scan_regex` `:654-673`, `scan_entropy` `:605-627`, `evaluate_entropy_string` `:629-652`, `signature_is_excluded` `:420-430`, `calculate_entropy` `:494-514`, entropy limits `:181-211`) and `tartufo/tartufo/util.py` (`generate_signature` `:274-283`, `find_strings_by_regex` `:302-319`). This spec re-implements that behavior from its description as a pure, git-free `detect` package, adapted to gelato's decision to scan only added lines. Signatures must match tartufo exactly, or every user's `exclude-signatures` stops working.

---

## Changes

### 1. Types (`detect/detect.go`)

```go
// Chunk is the added content of one file in one change.
type Chunk struct {
	Path  string   // repository-relative, forward slashes
	Lines []string // added lines, without the leading "+", in diff order
}

type IssueType string // values "Regular Expression" and "High Entropy" (tartufo's IssueType values)

type Issue struct {
	Type      IssueType
	Detail    string // rule name for regex issues, "" for entropy
	Match     string
	Path      string
	Signature string
	Chunk     Chunk // the chunk it was found in, for output
}

type Rule struct {
	Name        string
	Pattern     *regexp.Regexp
	PathPattern *regexp.Regexp // nil = every path; otherwise applied start-anchored, like re.match
}
```

`Chunk` carries no git or commit data. The git layer (spec 4) and the commands (specs 5, 6) keep commit context next to the chunk, so `detect` has no dependency on them.

### 2. Signature (`detect/signature.go`)

`func Signature(match, path string) string` returns the lowercase hex BLAKE2s-256 digest of the UTF-8 bytes of `match + "$$" + path`, using `golang.org/x/crypto/blake2s` (BSD-3, pure Go). Same format as `util.py:274-283`. Golden value, computed with Python's `hashlib.blake2s`: `Signature("not-a-real-secret", "config/app.py") == "0e027678bd911189618fd0c11817c6900877b3933bcd54dea457f5e7029acebe"`. Golden inputs are never token-shaped (spec 3's repository-wide check).

A match is excluded when either the match itself or its signature is in `cfg.Config.ExcludeSignatures` (`scanner.py:420-430`: tartufo also checks the raw string, because a signature written into a config file shows up as a high-entropy string).

### 3. Engine construction (`detect/engine.go`)

`func New(c cfg.Config, rules []Rule) (*Engine, error)`:

- Rules are sorted by `Name`, so output order doesn't depend on map or set order (tartufo uses a `set`, so its order varies).
- A rule whose `Pattern` has more than one capture group is an error: tartufo's `findall` returns tuples in that case (`scanner.py:662`), and no rule needs it.
- The `*Engine` is immutable after `New` and safe for concurrent use by many goroutines (`regexp.Regexp` is goroutine-safe), which the worker pools in specs 5 and 6 rely on. `New` clones the rules and the config's `ExcludeSignatures` map and exclusion slices, so later changes by the caller don't reach the engine.

### 4. `func (e *Engine) Scan(c Chunk) []Issue`

1. **Path exclusion**: if any `cfg.Config.ExcludePaths` pattern matches `c.Path`, return nothing. In tartufo this happens before a chunk exists (`scanner.py:348-372`); keeping it in the engine means the git layer stays config-free.
2. **Regex** (if `Regex` is on): the scanned text is `strings.Join(c.Lines, "\n")`, matching tartufo running `findall` over the whole chunk (`scanner.py:662`), so `\s` can cross line breaks. For each rule (by name), skip it if `PathPattern != nil` and doesn't match the path. For each non-overlapping match, the result is capture group 1 if the pattern has one group, else the whole match. Drop it if its signature is excluded; otherwise emit an issue with `Detail = rule.Name`.
3. **Entropy** (if `Entropy` is on), after regex as in tartufo (`scanner.py:591-600`). For each non-empty line, then each whitespace-separated word. Words are split with `strings.FieldsFunc` on `unicode.IsSpace(r) || (r >= 0x1C && r <= 0x1F)`, because Python's `str.split()` also treats U+001C–U+001F as whitespace and `unicode.IsSpace` doesn't.
   - base64 candidates are maximal runs of `[A-Za-z0-9=+/_\x{0130}\x{0131}\x{017F}\x{212A}-]+` and hex candidates maximal runs of `[0-9A-Fa-f]+`. These are exactly what tartufo's `re.IGNORECASE` classes `[A-Z0-9=+/_-]` and `[0-9A-F]` match (`scanner.py:43-44`): Python also folds `İ`, `ı`, `ſ` and `K` (Kelvin) into `A-Z`, and Go's `(?i)` misses `İ` and `ı`. A word yields both kinds.
   - Keep candidates with at least 20 runes (`util.py:302-319`).
   - Flag a candidate if its Shannon entropy over runes is **strictly greater** than the limit: base64 `0.75 × 6 = 4.5`, hex `0.75 × 4 = 3.0` (fixed sensitivity 75, `scanner.py:181-211`).
   - Skip it if its signature is excluded (checked first, as in `scanner.py:645`), or if any `ExcludeEntropyPatterns` entry has `Pattern` matching the **line** and `PathPattern` matching the path, both unanchored (line scope and search semantics, `scanner.py:434-478`).
4. **Within-chunk dedup**: identical `(Type, Detail, Match)` triples are reported once. Tartufo reports each occurrence; repeats in the same added block add noise without new information.

Issues come back in a deterministic order: regex issues by rule name then position, then entropy issues by line, then word, with a word's base64 candidates before its hex candidates (tartufo's order, so a hex run can follow a later base64 run in the same word).

### 5. Python-compatibility notes (doc comments plus tests, no emulation)

- Lengths and entropy count runes, as Python counts code points, so the non-ASCII characters in the base64 class are measured like Python does.
- Rule patterns stay unanchored at the end; Python `$` also matching before a trailing `\n` is irrelevant because lines carry no newline.

### 6. Tests (`detect/*_test.go`, table- and testdata-driven)

- Signature: the golden above, plus a non-ASCII path and match.
- Entropy: goldens generated with Python (e.g. `ZWxsbyB3b3JsZCBoZWxsbyB3b3JsZA` → `3.41506101220307`), with the boundary at exactly the limit not flagged.
- Tokenizing: 19 vs 20 runes, a word with both base64 and hex runs, punctuation splitting runs, `İ`/`ı`/`ſ`/`K` inside a base64 run, U+001C splitting words.
- Exclusions: raw-string signature, computed signature, entropy pattern requiring both line and path match, empty `path-pattern`, path exclusion short-circuiting both scans.
- Regex: group-1 extraction, whole-match extraction, multi-group rejection at `New`, `PathPattern` filtering, matching across `\n`.
- Engine with `Regex` or `Entropy` off.
- Changing the caller's `Config` exclusions after `New` doesn't change `Scan` results.
- Entropy order within a word: base64 candidates, then hex.
- `go test -race` with a goroutine fan-out over one `*Engine`.

---

## What does NOT change

- No git, output, or CLI code; `command.PreCommit`/`command.Scan` remain stubs.
- No `exclude-regex-patterns`, `include-path-patterns`, `rule-patterns`, or configurable entropy sensitivity.
- The default rule set is spec 3; this spec tests with rules defined in test code only.

---

## Acceptance Criteria

- [ ] `detect.Signature` reproduces the Python golden value above and every golden in `detect/testdata/`.
- [ ] A 30-character base64 string with entropy > 4.5 in an added line is reported once; the same line in a path matched by `exclude-path-patterns` is not.
- [ ] A matched string is suppressed when either the string or its signature is in `exclude-signatures`.
- [ ] An `exclude-entropy-patterns` entry suppresses a finding only when both its `pattern` matches the line and its `path-pattern` matches the path.
- [ ] `detect.New` rejects a rule with two capture groups.
- [ ] Repeated `Scan` calls on the same input return identical issue slices in identical order.
- [ ] `make all` passes, including the `-race` concurrency test.

---

## Files to Change

| File | Change |
|---|---|
| `go.mod`, `go.sum` | Add `golang.org/x/crypto` |
| `detect/detect.go` | `Chunk`, `Issue`, `IssueType`, `Rule` |
| `detect/signature.go` | `Signature` and exclusion check |
| `detect/engine.go` | `New`, `Engine.Scan`, tokenizing, entropy |
| `detect/signature_test.go` | Signature goldens |
| `detect/engine_test.go` | Regex, entropy, exclusion, ordering, race tests |
| `detect/testdata/*` | Python-generated golden signatures and entropy values |
| `tartufo.toml` | gelato's own config, so a self-scan is clean: excludes `go.sum` and `detect/testdata/signatures.json` by path, and the signature golden above as a raw string |

---

## Risk

An off-by-one in tokenizing or the strict `>` threshold would make findings differ from tartufo and leave existing `exclude-signatures` mismatched; the Python-generated goldens and the parity run in spec 7 act as the guards.

---

## Out of scope / follow-up

- Filename scanning is implemented by the commands adding the path as an extra one-line chunk (spec 5). Its signatures will differ from tartufo's, whose filename findings came from `a/…`/`b/…` diff-header tokens.
