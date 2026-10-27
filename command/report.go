package command

import (
	"fmt"
	"io"
	"strings"

	"github.com/satoseino/gelato/detect"
)

type (
	// reporter prints issues in tartufo's text layout.
	reporter struct {
		w     io.Writer
		color bool
	}

	// field is a labeled line of an issue's metadata, such as its commit.
	field struct {
		label string
		value string
	}
)

const (
	// issueSeparator opens and closes each issue, as in tartufo.
	issueSeparator = "~~~~~~~~~~~~~~~~~~~~~"

	// ANSI SGR sequences for click's "bright_green" and "bright_yellow",
	// which tartufo uses for its labels and for the match.
	styleOK      = "\x1b[92m"
	styleWarning = "\x1b[93m"
	styleReset   = "\x1b[0m"
)

// newReporter returns a reporter writing to w, colored as useColor decides.
func newReporter(g *Globals, w io.Writer) reporter {
	return reporter{w: w, color: useColor(g, w)}
}

// issue prints i followed by meta. Unlike tartufo, which prints the whole
// diff chunk, the body is only the added lines that contain the match, each
// prefixed with "+"; a match that spans lines prints every line of the chunk.
func (r reporter) issue(i detect.Issue, meta []field) error {
	var b strings.Builder

	b.WriteString(issueSeparator + "\n")
	r.line(&b, "Reason: "+string(i.Type))

	if i.Detail != "" {
		r.line(&b, "Detail: "+i.Detail)
	}

	r.line(&b, "Filepath: "+i.Path)
	r.line(&b, "Signature: "+i.Signature)

	for _, f := range meta {
		r.line(&b, f.label+": "+f.value)
	}

	body := strings.Join(bodyLines(i), "\n")
	if i.Match != "" {
		body = strings.ReplaceAll(body, i.Match, r.highlight(i.Match))
	}

	for l := range strings.SplitSeq(body, "\n") {
		b.WriteString("+" + l + "\n")
	}

	b.WriteString(issueSeparator + "\n")

	_, err := io.WriteString(r.w, b.String())

	return err
}

// summary prints the line that ends a report of n issues.
func (r reporter) summary(n int) error {
	var err error
	if n == 0 {
		_, err = io.WriteString(r.w, "All clear. No secrets detected.\n")
	} else {
		_, err = fmt.Fprintf(r.w, "Found %d issue(s).\n", n)
	}

	return err
}

// line writes s as a bright green line.
func (r reporter) line(b *strings.Builder, s string) {
	if r.color {
		s = styleOK + s + styleReset
	}

	b.WriteString(s + "\n")
}

// highlight styles match bright yellow, one line at a time, so that every
// body line keeps its own "+" prefix outside the escape sequences.
func (r reporter) highlight(match string) string {
	if !r.color {
		return match
	}

	segments := strings.Split(match, "\n")
	for k, s := range segments {
		if s != "" {
			segments[k] = styleWarning + s + styleReset
		}
	}

	return strings.Join(segments, "\n")
}

// bodyLines returns the lines of i's chunk that an issue's body shows.
func bodyLines(i detect.Issue) []string {
	if strings.Contains(i.Match, "\n") {
		return i.Chunk.Lines
	}

	var lines []string

	for _, l := range i.Chunk.Lines {
		if strings.Contains(l, i.Match) {
			lines = append(lines, l)
		}
	}

	return lines
}
