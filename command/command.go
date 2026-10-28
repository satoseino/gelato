// Package command implements gelato's subcommands as kong command structs.
package command

import (
	"errors"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Globals holds the flags shared by every command.
type Globals struct { //nolint:govet // fieldalignment: kong lists flags in field order.
	Config       []string `help:"Read configuration from this file. Repeatable; list values are concatenated in order." name:"config" sep:"none" type:"path"`
	NoColor      bool     `help:"Disable colored output."                                                                 name:"no-color"`
	OutputFormat string   `default:"text" enum:"text,json" help:"Report format: \"text\" for people, or \"json\" for scripts and AI agents (exactly one JSON document on stdout). See \"gelato <command> --help\" for the JSON fields and how to resolve a finding." name:"output-format"`
}

// The --output-format values.
const (
	formatText = "text"
	formatJSON = "json"
)

// ErrFindings is returned by a command that ran successfully but found
// secrets. It is the only error that maps to exit code 1.
var ErrFindings = errors.New("secrets found")

// useColor reports whether output to w is colored: only when it is a
// terminal, as tartufo decides, and neither --no-color nor a non-empty
// NO_COLOR environment variable (https://no-color.org) turns it off.
func useColor(g *Globals, w io.Writer) bool {
	if g.NoColor || os.Getenv("NO_COLOR") != "" {
		return false
	}

	f, ok := w.(*os.File)

	return ok && term.IsTerminal(int(f.Fd()))
}

// findingHelp returns the guide to reading and resolving a finding that
// pre-commit --help and scan --help print, in go/doc comment syntax. scan
// selects the parts that only apply to a history scan.
func findingHelp(scan bool) string {
	var b strings.Builder

	b.WriteString(`Exit codes: 0 when nothing is found, 1 when secrets are found, and 2 for
any other error, which is printed on stderr.

With --output-format=json, stdout is exactly one JSON document,
{"found_issues": [...]}, even when nothing is found. On exit code 2, stdout
is empty. Each issue has these keys:

  - file_path: the repository-relative path the string was added to.
  - matched_string: the exact flagged string, which may span lines.
  - signature: the finding's ID for exclude-signatures, computed from
    matched_string and file_path only.
  - issue_type: "Regular Expression" or "High Entropy".
  - issue_detail: the regex rule's name, or null for High Entropy.
  - lines: the added lines containing matched_string (every added line of the
    change when it spans lines). A line longer than 500 characters is cut to
    80 characters on each side of the match, or to its first 80 characters
    when it holds none of the match, and "…" marks each cut.
`)

	if scan {
		b.WriteString(`  - commit_hash, commit_time, commit_message: the oldest commit that added
    the finding.
`)
	}

	b.WriteString(`
To resolve a finding, first decide whether matched_string is a real secret.

If it is, remove it from the file and rotate it at its issuer.`)

	if scan {
		b.WriteString(` Removing it
from the file doesn't remove it from the repository's history.`)
	}

	b.WriteString(`

If it isn't, add one entry under [tool.tartufo] in tartufo.toml (or
pyproject.toml), with a reason. The default choice suppresses exactly this
string in this file:

    exclude-signatures = [{signature = "<signature>", reason = "<why this is not a secret>"}]

For files that are entirely generated or test data, skip every finding in
files whose path the regex matches from the start of file_path:

    exclude-path-patterns = [{path-pattern = '<regex>', reason = "<why>"}]

For High Entropy issues only, suppress every high-entropy string on a line
that pattern matches anywhere, in files whose path path-pattern matches
anywhere; path-pattern is optional:

    exclude-entropy-patterns = [{pattern = '<regex>', path-pattern = '<regex>', reason = "<why>"}]

Prefer the narrowest entry. Never put matched_string itself in the config:
exclude-signatures also accepts the raw string, and that would commit the
secret.
`)

	return b.String()
}
