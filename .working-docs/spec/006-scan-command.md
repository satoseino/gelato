# Implement `gelato scan`: scan added lines across the repository's history

## Background

After specs 1–5, `command.Scan` (`command/scan.go`) resolves `cfg.Config.RepoPath` and stops at `Run() → "scan: not implemented"`. Tartufo's equivalent is `scan-local-repo` (`tartufo/tartufo/commands/scan_local_repo.py`) driving `GitRepoScanner.chunks` (`tartufo/tartufo/scanner.py:877-945`) and `_get_chunks` (`:827-875`). Tartufo walks every local and remote branch separately and diffs each commit against its first parent, deduplicating parent/commit pairs with an MD5 set. Per branch, it diffs only the *last* commit visited against the empty tree, and in a shallow clone it scans only HEAD's full tree. This spec implements history scanning on spec 4's git layer with one `rev-list`, parallel `diff-tree` processes and spec 5's reporter. The `diff-tree --stdin` behavior relied on below was checked against git 2.56.0.

---

## Changes

### 1. Commit enumeration — `git/history.go`

`func (r *Repo) Commits(ctx context.Context) ([]Commit, error)`, with `type Commit struct { ID, FirstParent string }`:

- Runs `git rev-list --topo-order --parents --branches --remotes [HEAD]`. Local and remote-tracking branches match tartufo's `repo.branches` (`scanner.py:905`), and tags are excluded as in tartufo. `HEAD` is added only if `git rev-parse --verify -q HEAD` succeeds, so a detached checkout (typical in CI) is covered and an empty repository returns no commits instead of an error.
- `rev-list` lists each commit once, which replaces tartufo's per-branch walk plus MD5 dedup.
- `FirstParent` is the second field of each line, or `""` for root commits **and for shallow-boundary commits** (git reports their parents as absent). Every commit with no parent is diffed against the empty tree. This covers the case tartufo special-cased for shallow clones (`scanner.py:896-900`) and fixes its scanning of only one root when several exist (`:864-875`).

### 2. Patch streaming — `git/history.go`

`func (r *Repo) Patches(ctx context.Context, commits []Commit) iter.Seq2[FileChange, error]` runs one `git diff-tree --stdin --root` with spec 4's pinned patch options, through spec 4's runner with a stdin reader. It writes `"<ID> <FirstParent>\n"` (or `"<ID>\n"` for parentless commits) to stdin and parses stdout with `ParsePatch`, which sets `FileChange.Commit` from the object-ID line git prints before each commit's diff.

- The `<commit> <parent>` form diffs a merge against its first parent only, as tartufo does (`scanner.py:838`, `prev_commit = curr_commit.parents[0]`). Without the explicit parent, `diff-tree` prints nothing for merges.
- A merge re-shows side-branch content against the first parent; cross-commit dedup (§4) keeps that from being reported twice.

### 3. `command.Scan.Run` — `command/scan.go`

1. `git.Open(c.cfg.RepoPath, git.WithoutRepoEnv())`, so an inherited `GIT_DIR` or `GIT_INDEX_FILE` can't redirect the scan.
2. `detect.New(c.cfg, detect.DefaultRules())`.
3. `Commits`. If there are none, print "All clear" and return nil.
4. Split the commit list into `min(runtime.GOMAXPROCS(0), len(commits))` contiguous batches. Run one `Patches` stream per batch concurrently; independent read-only git processes are safe to run in parallel.
5. Turn `FileChange`s into chunks exactly as `pre-commit` does (spec 5, including the separate path chunk for `Added`/`Renamed`), then scan them on the shared engine worker pool. Factor that conversion and the pool into a shared helper in `command/` used by both commands.
6. Tag every issue with `(commit index in rev-list order, chunk sequence within the commit)`.

### 4. Cross-commit dedup and ordering

- Issues with the same `(Signature, Type, Detail)` are reported once, at the **oldest** commit containing them (highest rev-list index), which is where the secret was introduced. Tartufo reports every occurrence; one `exclude-signatures` entry already covers all of them, so repeats only add noise.
- Kept issues are printed in rev-list order (newest first), then chunk and issue order. The output is byte-identical between runs regardless of batch timing.

### 5. Commit metadata in output

For commits with at least one kept issue, `func (r *Repo) CommitInfo(ctx context.Context, ids []string) (map[string]CommitInfo, error)` in `git/history.go` makes one `git log --no-walk=unsorted --stdin --no-show-signature --encoding=UTF-8 --format=%H%x00%cI%x00%s` call to read hash, committer date and subject. `--no-show-signature` and `--encoding=UTF-8` pin what `log.showSignature` and `i18n.logOutputEncoding` would otherwise change (checked with git 2.56.0). The reporter prints them as meta fields `Commit hash`, `Commit time` and `Commit message`, the labels tartufo derives from `extract_commit_metadata` (`tartufo/tartufo/util.py:286-299`). It uses the subject line instead of the full message, and no `Branch` (one `rev-list` has no single branch per commit).

### 6. Tests — `git/history_test.go`, `command/scan_test.go`

End-to-end on repositories from `internal/gittest` (spec 4), with fragment-built secrets:

- A secret added in commit 2 and removed in commit 4: reported once with commit 2's metadata.
- A secret on a side branch that is later merged: reported once, at the side-branch commit.
- Secrets only reachable from a remote-tracking ref (`git update-ref refs/remotes/origin/x`), only from a detached `HEAD`, and in each of two root commits (orphan branch): all reported.
- A `--depth 1` clone (`file://` URL) whose HEAD tree contains a secret added in an older commit: reported, with the shallow commit diffed against the empty tree.
- An empty repository: exit 0, "All clear".
- Submodule entries and binary files in history: skipped.
- `REPO_PATH` argument vs `repo-path` from `--config` vs default `.`; an inherited `GIT_DIR` pointing elsewhere is ignored.
- Byte-identical output across repeated runs on a repository with enough commits to need several batches.

---

## What does NOT change

- No `--branch`, `--since-commit`, `--max-depth`, `--progress`, or tag scanning.
- No scanning of removed or context lines.
- No bare-repository support: `git rev-parse --show-toplevel` (spec 4) fails there, as it does for any non-worktree path.

---

## Acceptance Criteria

- [ ] `gelato scan` on a repository where a fragment-built secret was added and later removed exits 1 and reports it once with that commit's hash, time and subject.
- [ ] A secret introduced on a side branch and merged appears exactly once.
- [ ] A secret reachable only from `refs/remotes/...` or a detached `HEAD` is reported.
- [ ] `gelato scan` in a `--depth 1` clone reports secrets present in HEAD's tree.
- [ ] `gelato scan` on a repository with no commits exits 0.
- [ ] Repeated runs produce byte-identical stdout.
- [ ] On `jarvis-registry`, `gelato scan` completes and its wall time is recorded in the spec 7 parity notes alongside tartufo's.
- [ ] `make all` passes.

---

## Files to Change

| File | Change |
|---|---|
| `git/history.go` | `Commit`, `Commits`, `Patches`, commit metadata via `git log` |
| `git/history_test.go` | Enumeration and streaming tests (roots, merges, shallow, empty repo) |
| `command/scan.go` | Real `Run`: batching, dedup, ordering, metadata, reporting |
| `command/pipeline.go` | Shared `FileChange` → chunk conversion and engine worker pool, used by both commands |
| `command/precommit.go` | Use the shared pipeline helper |
| `command/scan_test.go` | End-to-end history tests |

---

## Risk

Batching plus cross-commit dedup is where nondeterminism or wrong "introduced in" attribution could creep in; the merge, add-then-remove and repeated-run tests are the guards.
