# Add the git CLI layer: pinned invocations, patch parser, staged diff

## Background

gelato gets every patch from the `git` CLI instead of a git library (`CLAUDE.md`, "Project constraints"). Tartufo's equivalent is pygit2 code in `tartufo/tartufo/scanner.py` (`_iter_diff_index` `:716-751`, `header_length` `:753-765`, `GitPreCommitScanner.chunks` `:974-991`), which skips deleted and binary files and strips diff headers. This spec adds a `git` package that runs git safely, parses `--unified=0` patch streams into per-file added lines, and exposes the staged diff for spec 5; spec 6 reuses the runner and parser for history. Output formats below were checked against git 2.56.0.

---

## Changes

### 1. `git.Open(dir string, opts ...Option) (*Repo, error)` — `git/repo.go`

- Finds `git` with `exec.LookPath`; a missing binary is an error: `git executable not found in PATH`.
- Runs `git version` (in the current directory, not `dir`) and parses the first three numeric components, accepting suffixes such as `2.39.3 (Apple Git-146)` and `2.45.1.windows.1`. Below 2.30 is an error naming the found version and the minimum.
- Runs `git rev-parse --show-toplevel` in `dir`. Failure, including a `dir` that doesn't exist, becomes `<dir> is not a valid git repository: <git stderr>` (tartufo's wording, `commands/scan_local_repo.py:63`). `Repo.Root()` returns the result.
- **Environment**: by default the child inherits the whole environment, so a hook's `GIT_INDEX_FILE` (git sets it to a temporary index for `git commit -a` and `git commit <paths>`) reaches `git diff --cached`. `git.WithoutRepoEnv()` instead removes the repository-routing variables (`GIT_DIR`, `GIT_WORK_TREE`, `GIT_INDEX_FILE`, `GIT_OBJECT_DIRECTORY`, `GIT_ALTERNATE_OBJECT_DIRECTORIES`, `GIT_COMMON_DIR`, `GIT_PREFIX`, `GIT_SHALLOW_FILE`, `GIT_GRAFT_FILE`, `GIT_NO_REPLACE_OBJECTS`, `GIT_REPLACE_REF_BASE`, `GIT_IMPLICIT_WORK_TREE`, `GIT_CONFIG`, `GIT_CONFIG_PARAMETERS`, `GIT_CONFIG_COUNT` — the list from `git rev-parse --local-env-vars`). `scan` uses it so an explicit `REPO_PATH` isn't overridden when gelato runs inside another repository's hook.
- `GIT_OPTIONAL_LOCKS=0` is always set, so git never takes `index.lock` while `git commit` holds it.

### 2. Pinned invocation — `git/repo.go`

Every git call goes through one unexported helper that prepends:

```
-c core.quotePath=false -c diff.noprefix=false -c core.fsmonitor=false
```

and every patch-producing call (`diff`, `diff-tree`) also gets:

```
-p --no-color --no-ext-diff --no-textconv --no-relative
--src-prefix=a/ --dst-prefix=b/ -M50% --unified=0
--inter-hunk-context=0 --diff-algorithm=myers --submodule=short
```

Why each matters: user or repo config can recolor output (`color.ui`), drop or change prefixes (`diff.noprefix`, `diff.mnemonicPrefix`), make paths cwd-relative (`diff.relative`), turn submodule entries into commit logs (`diff.submodule=log`), change which lines count as added (`diff.algorithm`; Myers is libgit2's default), or run arbitrary programs (`diff.external`, textconv drivers, `core.fsmonitor`). `-M50%` matches libgit2's default rename threshold, so renamed paths match tartufo's history scan (`scanner.py:855`). `--unified=0` means a patch contains only changed lines, and `--inter-hunk-context=0` keeps it that way: `diff.interHunkContext` would otherwise join nearby hunks with context lines, which `diff.suppressBlankEmpty` prints as empty lines the parser rejects.

Stdout is streamed (never buffered whole). The helper takes an optional `io.Reader` for stdin, which spec 6 uses to feed `diff-tree --stdin` and `log --stdin`, so every process launch stays in `git/repo.go`. Stderr is captured, and a non-zero exit returns `git <subcommand> failed: <exit status>: <stderr>`. A `context.Context` cancels the process.

### 3. Patch parser — `git/patch.go`

`func ParsePatch(r io.Reader) iter.Seq2[FileChange, error]`:

```go
type FileChange struct {
	Commit string // object ID from a diff-tree --stdin header line; "" for staged diffs
	Path   string // new path, repository-relative
	Status Status // Added, Modified, Renamed
	Lines  []string
}
```

- Reads with `bufio.Reader` (no `bufio.Scanner` token-size limit: minified files can have multi-megabyte lines).
- A line that is only a 40- or 64-character hex object ID sets the current commit. It can't appear anywhere else in `-U0` output, where body lines start with `+`, `-`, ` `, `\`, `@@` or `diff --git`.
- File headers: `diff --git`, then extended headers (`new file mode`, `deleted file mode`, `old mode`/`new mode`, `similarity index`, `rename from`/`rename to`, `index <a>..<b>[ <mode>]`), then `---`/`+++`. The path comes from `+++ b/<path>`. Without a `+++` line it comes from `rename to <path>` (pure renames) or, for new empty files and mode-only changes, from the `diff --git` header, whose two paths are then identical: an unquoted `a/<p> b/<p>` is split at its midpoint (`<p>` may contain spaces, e.g. `diff --git a/e mpty b b/e mpty b`), and a quoted header is two C-quoted strings (checked with git 2.56.0).
- Path decoding: quoted paths (`"b/t\"q.txt"`) are C-unquoted with `strconv.Unquote`, which handles git's escapes and octal bytes. Unquoted `---`/`+++` paths containing spaces carry a trailing TAB that git adds (seen with `+++ b/sp ace.txt<TAB>`), which is stripped.
- A type change (e.g. file to symlink) appears in patch output as a deletion plus a `new file mode` entry for the same path (checked with git 2.56.0), so it comes back as `Added`; there is no separate type-change status.
- Hunks are consumed by count: the `@@ -a[,b] +c[,d] @@[ <context>]` header (which may end in function-context text) gives how many old and new lines follow, and exactly that many `-`/`+`/` ` lines are read, ignoring `\ No newline at end of file`. Without counting, an added line whose content starts with `++ ` (`+++ …`) or a removed line starting with `-- ` (`--- …`) would be mistaken for a file header.
- Added lines are the `+` lines inside hunks, without the `+`. Content stays byte-for-byte except that invalid UTF-8 is replaced with `U+FFFD` (`strings.ToValidUTF8`), since tartufo scanned decoded text and signatures hash UTF-8.
- **Dropped, never yielded**: deleted files; binary files (`Binary files … differ`); submodules (any mode `160000` in the extended headers); `Modified` changes with no added lines. `Added` and `Renamed` changes are yielded even with no added lines, because spec 5 scans their path.

### 4. Staged changes — `git/repo.go`

`func (r *Repo) Staged(ctx context.Context) iter.Seq2[FileChange, error]` runs `git diff --cached` with the pinned options and parses it. On an unborn branch (no commits yet), git compares the index against the empty tree, so every staged file comes back as `Added`. No special case is needed; tartufo diffs against `"HEAD"` (`scanner.py:984`).

### 5. Tests

- `git/patch_test.go`, table-driven over `git/testdata/*.patch` captured from real git: added/modified/renamed/type-changed files, quoted and TAB-suffixed paths, binary, submodule, deletion, mode-only change, `\ No newline`, CRLF content, invalid UTF-8, an added line starting with `++ ` and a removed line starting with `-- `, hunk headers with function context, multiple commits from `diff-tree --stdin`.
- `internal/gittest/gittest.go`: the shared helper that builds temporary repositories with the real `git` binary, used here and by specs 5 and 6. It isolates user config (`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, fixed author, `init.defaultBranch=main`) and runs arbitrary git commands in the repository (commit, branch, clone, `update-ref`).
- `git/repo_test.go`, end-to-end against repositories from `internal/gittest`. Cases:
  - staged diff on an unborn branch;
  - staged rename plus edit;
  - a new empty file whose name contains a space (`Added`, path from the `diff --git` header);
  - repo config `diff.noprefix=true`, `diff.submodule=log`, `diff.relative=true`, `diff.interHunkContext=3`, `diff.suppressBlankEmpty=true` and `color.ui=always` not changing output;
  - `GIT_INDEX_FILE` pointing at an alternate index being honored by default and ignored with `WithoutRepoEnv`;
  - not-a-repository (including a missing directory) and old-version errors (the version parser tested directly).
- Fixture contents are plain words, never token-shaped strings.

### 6. Lint exclusion — `.golangci.yaml`

Add a `linters.exclusions.rules` entry for `path: '^git/repo\.go$'`, `linters: [gosec]`, `text: 'G204: Subprocess launched with variable'`, with the comment "git is the fixed executable; arguments are built from pinned option lists, object IDs and repository paths, never passed to a shell". A matching entry covers `internal/gittest/gittest.go` (test fixtures run git with test-chosen arguments).

### 7. Bring `CLAUDE.md` "Project constraints" in line with this layer

- Replace the command list `(for-each-ref, rev-list --topo-order, diff-tree -p, diff --cached)` with `(rev-list, diff-tree --stdin, diff --cached, log)`; gelato never calls `for-each-ref`.
- Replace the pinned-option sentence with the full lists from §2 and add `GIT_OPTIONAL_LOCKS=0`.
- Replace "Inherit the environment (a hook's `GIT_INDEX_FILE` must reach `git diff --cached`)" with "`pre-commit` inherits the environment (a hook's `GIT_INDEX_FILE` must reach `git diff --cached`); `scan` strips the repository-routing variables (`git.WithoutRepoEnv`)."

---

## What does NOT change

- No history traversal (`rev-list`, `diff-tree` invocation): spec 6. This spec only makes the parser understand `diff-tree --stdin` commit lines.
- No turning `FileChange` into `detect.Chunk`, including the filename line: spec 5.
- No go-git or other git library.

---

## Acceptance Criteria

- [ ] `git.Open` on a non-repository returns an error containing `is not a valid git repository`.
- [ ] With repo config `diff.noprefix=true`, `diff.submodule=log`, `diff.relative=true`, `diff.interHunkContext=3`, `diff.suppressBlankEmpty=true` and `color.ui=always`, `Staged` returns the same `FileChange`s as without them.
- [ ] Staging a new file on an unborn branch yields one `Added` change with all its lines.
- [ ] A staged binary file, a deleted file and a submodule bump yield nothing.
- [ ] A path containing a space, a quote and a non-ASCII character round-trips exactly.
- [ ] A hook-style `GIT_INDEX_FILE` pointing at an alternate index changes `Staged`'s result by default and not under `WithoutRepoEnv()`.
- [ ] `make all` passes with only the scoped `G204` exclusions added.

---

## Files to Change

| File | Change |
|---|---|
| `git/repo.go` | `Open`, options, environment handling, version check, pinned runner, `Staged` |
| `git/patch.go` | `FileChange`, `Status`, `ParsePatch` |
| `git/patch_test.go` | Table-driven parser tests |
| `git/repo_test.go` | End-to-end tests against temporary repositories |
| `internal/gittest/gittest.go` | Shared isolated temporary-repository helper |
| `git/testdata/*.patch` | Captured patch fixtures |
| `.golangci.yaml` | Scoped `G204` exclusions for `git/repo.go` and `internal/gittest/gittest.go` |
| `CLAUDE.md` | Git command list, pinned options and environment rule |

---

## Risk

A git config key not pinned here could still change the patch format and make the parser silently skip content; the end-to-end test that applies hostile repo config is the guard, and new keys should be added there when found.
