# gelato

gelato scans the lines added to a git repository for secrets: staged changes before a commit (`gelato pre-commit`) or the whole history (`gelato scan`). It reads [tartufo](https://github.com/godaddy/tartufo)'s `[tool.tartufo]` config and produces the same finding signatures, so a team using tartufo can switch without touching its `tartufo.toml`.

gelato is an independent implementation under the MIT License, written in Go. It is not a fork of tartufo and includes none of tartufo's code or data.

## Install

gelato needs `git` 2.30 or newer on `PATH`.

### pre-commit hook

```yaml
# .pre-commit-config.yaml
repos:
  - repo: https://github.com/satoseino/gelato
    rev: vX.Y.Z
    hooks:
      - id: gelato
```

pre-commit builds the hook from source with Go (`language: golang`), downloading a Go toolchain into its own environment when none is available. The build is cached per `rev`, so upgrading Python doesn't break the hook.

To run a `gelato` you installed with [Homebrew](#homebrew-macos-and-linux) or from a [release binary](#binary) instead, use `id: gelato-system`. It needs pre-commit 4.4.0 or newer and builds nothing; `rev` then selects only the hook definition, and the `gelato` on `PATH` decides the version that runs.

### Homebrew (macOS and Linux)

```sh
brew install satoseino/tap/gelato
```

### Binary

Download the archive for your platform from [GitHub Releases](https://github.com/satoseino/gelato/releases), check it against `checksums.txt`, and put `gelato` (`gelato.exe` on Windows) on your `PATH`.

## Usage

```sh
gelato pre-commit             # scan lines added in staged changes
gelato scan [<repo-path>]     # scan lines added across the repository's history
```

| Flag | Meaning |
|---|---|
| `--config=FILE` | Read configuration from this file as well. Repeatable; list values are concatenated in order. |
| `--no-color` | Disable colored output. |
| `--output-format=text\|json` | `text` (default) for people; `json` prints exactly one `{"found_issues": [...]}` document on stdout. |
| `-V`, `--version` | Print the version and exit. |

Exit codes: `0` when nothing is found, `1` when secrets are found, `2` for any other error (printed on stderr).

`gelato <command> --help` lists the JSON fields and how to resolve a finding.

## Configuration

gelato reads the `[tool.tartufo]` table of `tartufo.toml`, or of `pyproject.toml` when there is no `tartufo.toml`, in the scanned repository, then every `--config` file. Supported keys:

| Key | Type | Meaning |
|---|---|---|
| `repo-path` | string | Repository to scan when `scan` is given no path (from `--config` files). |
| `regex` | bool | Scan with the regex rules (default `true`). |
| `entropy` | bool | Scan for high-entropy strings (default `true`). |
| `exclude-path-patterns` | array of `{path-pattern, reason}` | Skip files whose path the regex matches from its start. |
| `exclude-entropy-patterns` | array of `{pattern, path-pattern, reason}` | Suppress high-entropy findings on lines `pattern` matches, in files `path-pattern` matches (both unanchored; `path-pattern` optional). |
| `exclude-signatures` | array of `{signature, reason}` | Suppress the findings with these signatures. |

```toml
[tool.tartufo]
exclude-path-patterns = [
    {path-pattern = 'go\.sum$', reason = 'module checksums'},
]
exclude-entropy-patterns = [
    {pattern = 'sha256-[A-Za-z0-9+/=]+', path-pattern = 'package-lock\.json$', reason = 'integrity hashes'},
]
exclude-signatures = [
    {signature = '<signature from a report>', reason = 'test fixture, not a secret'},
]
```

Any other key, a wrong type or an invalid regex is an error naming the file and key, for example `tartufo.toml: exclude-entropy-patterns[0].scope: unknown key`.

## Differences from tartufo

- Only added lines are scanned.
- gelato has its own default regex rules, so the set of regex findings differs.
- Findings are deduplicated within a file change and, in `scan`, across commits; a finding is reported at the oldest commit that added it.
- Findings on file names have different signatures.
- `pre-commit` detects staged renames, so a renamed file's unchanged lines aren't scanned again.
- `regex` and `entropy` in the repository's own `tartufo.toml` take effect (tartufo honors them only from `--config`), and a config file that can't be parsed is an error rather than ignored.
- The report shows only the matching added lines, and a line longer than 500 characters is cut to a window around the match.
- JSON output uses tartufo's `found_issues` key names, except that `lines` (the windowed matching lines) replaces `diff`, and there is no `branch` key.
- `scan` covers all local branches, remote-tracking branches and `HEAD`, and diffs every root commit and shallow-boundary commit against the empty tree.

## Migrating from tartufo

1. In `.pre-commit-config.yaml`, replace the tartufo hook entry with gelato's:

   ```yaml
   - repo: https://github.com/satoseino/gelato
     rev: vX.Y.Z
     hooks:
       - id: gelato
   ```

2. Keep `tartufo.toml` (or the `[tool.tartufo]` table in `pyproject.toml`) as it is. Existing `exclude-signatures` entries keep working.
3. Run `gelato scan` once. Because the regex rules differ, expect some new regex findings; resolve each as `gelato scan --help` describes.
4. If gelato rejects a key, remove it. gelato supports only the keys listed under [Configuration](#configuration); tartufo options such as `default-regexes`, `rule-patterns`, `git-rules-repo`, `git-rules-files`, `include-path-patterns`, `exclude-regex-patterns`, `entropy-sensitivity`, `scan-filenames`, `output-format`, `output-dir`, `verbose`, `quiet` and `color`, and the `match-type` and `scope` fields of `exclude-entropy-patterns`, are rejected as unknown keys.

## License

MIT; see [LICENSE](LICENSE). Release archives include `THIRD_PARTY_LICENSES` with the notices of the Go modules and the Go standard library built into the binary.
