# gelato

Secret scanner for a single local git repository, distributed as a pre-commit hook and as a GoReleaser-built CLI. Reimplements the scanning behavior of `godaddy/tartufo` (reference clone in `tartufo/`) in Go.

## Project constraints

- Pure Go only; every build runs with `CGO_ENABLED=0`. All git access shells out to the `git` CLI (`rev-list`, `diff-tree --stdin`, `diff --cached`, `log`) — no git library (`go-git`, `git2go`/libgit2).
- Every `git` invocation pins the options that user/repo config could otherwise change or abuse: `-c core.quotePath=false -c diff.noprefix=false -c core.fsmonitor=false`, plus `-p --no-color --no-ext-diff --no-textconv --no-relative --src-prefix=a/ --dst-prefix=b/ -M50% --unified=0 --inter-hunk-context=0 --diff-algorithm=myers --submodule=short` on patch-producing commands (`diff`, `diff-tree`), and runs with `GIT_OPTIONAL_LOCKS=0`. `pre-commit` inherits the environment (a hook's `GIT_INDEX_FILE` must reach `git diff --cached`); `scan` strips the repository-routing variables (`git.WithoutRepoEnv`).
- gelato is MIT-licensed and tartufo is GPL-2.0-only: port tartufo's *behavior*, never its code, comments, docs, tests, or data files (including `tartufo/tartufo/data/default_regexes.json`). The default regex rules (`detect/rules.go`) are our own; each cites the provider documentation for its format, and test fixtures are assembled from fragments at runtime so the repository never contains a token-shaped string.
- Compatibility contract with existing tartufo users — changing any of these silently invalidates their `tartufo.toml`:
  - Signature: hex `blake2s("{matched_string}$${file_path}")`.
  - Config: `[tool.tartufo]` in `tartufo.toml` then `pyproject.toml`; keys `repo-path`, `regex`, `entropy`, `exclude-path-patterns`, `exclude-entropy-patterns`, `exclude-signatures`.
  - Python `re` semantics on Go `regexp`: `re.match` is start-anchored, `findall` returns group 1 when a pattern has one group, string lengths and entropy count runes, not bytes.
- Scan only added lines. Skip submodules (mode `160000`), deleted files, and binary files.
- Parallelize by running independent read-only `git` processes over disjoint commit ranges and scanning chunks in a worker pool; sort results so output is deterministic.

## Commands (kong)

Command structs use `github.com/alecthomas/kong`, which drives them through a three-phase DI lifecycle:

1. **`BeforeReset`** — set defaults that don't depend on parsed flags.
2. **`AfterApply`** — derive fields that depend on parsed flags or on data loaded at startup (e.g. config).
3. **`Run`** — assumes all dependencies are already set; if a dependency needs something only known at `Run` time, resolve it as the first step of `Run`.

Fields that need to be swapped out in tests — inject them as interface-typed or func-typed struct fields, not package-level vars.

## Config

Validate and resolve config at load time — fail loudly with a key-qualified error (e.g. `exclude-entropy-patterns[2].path-pattern: ...`), don't silently coerce or ignore unknown values.

## Package layout and naming

- No Go package folders that exist purely for organization. Every top-level Go package owns real behavior; use `internal/` only to keep an implementation detail out of the public API.
- Exported names read naturally with their package qualifier, no stutter (`scan.Issue`, not `scan.ScanIssue`). Read a new name back as `pkg.TypeName` before committing to it.

## Composition and interfaces

- Favor composition and small dedicated types for concerns that carry their own complexity or get reused elsewhere, rather than inlining everything into the type that uses them.
- Method count on an orchestrating type is not itself a smell — judge by interface depth instead (Ousterhout's "deep module": a simple external interface hiding non-trivial implementation). Extract a new type when private helpers start spanning *unrelated* responsibilities, not simply because there are a lot of them.
- Interfaces are defined at the consumer for DI/testability, not exported from the producer's package.

## Testing strategy

- Start every command with end-to-end test cases before smaller unit tests: drive the real `Run` method against a real git repository built in a temp dir, with hand-written stubs (not `testify`-generated mocks) for the few things that must be faked.
- Prefer `testdata/` fixture files over inline literals for inputs and expected outputs.
- Once an end-to-end baseline exists, add targeted cases for each subsequent change rather than re-deriving the whole flow.
- For functional code with few side effects — input in, output out (entropy, tokenizing, signatures, rule matching, config parsing) — use comprehensive table-driven and/or testdata-driven coverage.
- Cover happy paths thoroughly, but don't chase a coverage percentage. Don't add production abstractions (interfaces, func-typed fields) solely so a test can force an error branch that is rarely hit in actual execution. Test-only stubs of seams that already exist for other reasons (e.g. an `io.Writer` output field) are fine when the behavior they pin down is worth keeping, such as an error-message format.

## Local dev tooling

- Use the `Makefile` targets rather than calling `go test` / `golangci-lint` / etc. directly — `make all` (test + lint) is the default target and the bar for a change being done.
- `golangci-lint` must pass. Suppress a false positive case-by-case in `.golangci.yaml` under `linters.exclusions.rules`, scoped to the specific `path` and `text`, with a comment explaining why — never a blanket `//nolint` or a disabled linter. Exception: `govet`'s `fieldalignment` check emits a diagnostic whose text encodes struct-specific byte-count arithmetic, so it can't be pinned to a stable `text:` regex — suppress it with an inline, linter-scoped `//nolint:govet // fieldalignment: ...` comment on the struct instead, explaining why the field order is intentional.
- Import grouping and other style rules are auto-fixed by the lint/format tooling — run `make fmt` rather than hand-arranging imports.

## Error handling

Use `fmt.Errorf("...: %s", err.Error())` for context-only wrapping — this is the default. Use `%w` conservatively: wrapping with `%w` is a commitment to that error being part of the package's public interface. Reach for it specifically when callers need to distinguish *what kind* of failure occurred, not just *whether* one did — define an exported sentinel named `Err*` and wrap it with `%w`. If the underlying cause is itself a sentinel from another package (e.g. `go-git`'s `plumbing.ErrReferenceNotFound`), don't propagate that one directly; wrap your own `Err*` around it instead, so callers checking `errors.Is` against our API don't break if the underlying library changes. Don't mint a new sentinel for every fallible call — only for failure modes a caller would plausibly branch on.

## Doc comments

All new or changed exported identifiers require godoc-style comments.
