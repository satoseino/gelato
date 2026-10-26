// Package detect finds secrets in the lines added to a file, reproducing
// tartufo's regex and entropy checks and its finding signatures. It has no
// knowledge of git: callers hand it the added content as a Chunk.
package detect

import "regexp"

type (
	// Chunk is the added content of one file in one change.
	Chunk struct {
		// Path is repository-relative, with forward slashes.
		Path string
		// Lines are the added lines, without the leading "+", in diff order.
		Lines []string
	}

	// IssueType names the check that produced an Issue. Its values are
	// tartufo's, so output stays recognizable to tartufo users.
	IssueType string

	// Issue is one finding in a Chunk.
	Issue struct {
		Type IssueType
		// Detail is the rule name for a regex issue and "" for an entropy issue.
		Detail string
		// Match is the flagged string.
		Match string
		// Path is the path of the chunk the issue was found in.
		Path string
		// Signature identifies the finding in `exclude-signatures`.
		Signature string
		// Chunk is the chunk the issue was found in.
		Chunk Chunk
	}

	// Rule is a named regex check. When Pattern has a capture group, the
	// group's text is the match; otherwise the whole match is.
	Rule struct {
		Pattern *regexp.Regexp
		// PathPattern limits the rule to paths it matches, start-anchored like
		// Python's re.match. nil means every path.
		PathPattern *regexp.Regexp
		Name        string
	}
)

// The IssueType values.
const (
	RegexIssue   IssueType = "Regular Expression"
	EntropyIssue IssueType = "High Entropy"
)
