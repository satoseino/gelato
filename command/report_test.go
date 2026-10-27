package command

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/satoseino/gelato/detect"
)

func TestReporterIssue(t *testing.T) {
	tests := []struct {
		name  string
		issue detect.Issue
		meta  []field
	}{
		{
			name: "regex",
			issue: detect.Issue{
				Type: detect.RegexIssue, Detail: "Example rule", Match: "MATCH", Path: "dir/a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "dir/a.txt", Lines: []string{"one", "two MATCH two", "three"}},
			},
		},
		{
			name: "entropy",
			issue: detect.Issue{
				Type: detect.EntropyIssue, Match: "MATCH", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{"x MATCH y MATCH", "clean"}},
			},
		},
		{
			name: "repeated",
			issue: detect.Issue{
				Type: detect.EntropyIssue, Match: "MATCH", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{"MATCH one", "clean", "MATCH two"}},
			},
		},
		{
			name: "multiline",
			issue: detect.Issue{
				Type: detect.RegexIssue, Detail: "Multi-line rule", Match: "of\nthe", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{"before", "start of", "the match", "after"}},
			},
		},
		{
			name: "meta",
			issue: detect.Issue{
				Type: detect.RegexIssue, Detail: "Example rule", Match: "MATCH", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{"MATCH"}},
			},
			meta: []field{{label: "Commit hash", value: "0123abcd"}, {label: "Branch", value: "main"}},
		},
		{
			// The second occurrence falls inside the window, the third outside.
			name: "long",
			issue: detect.Issue{
				Type: detect.EntropyIssue, Match: "MATCH", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{
					"short",
					strings.Repeat("a", 300) + "MATCH" + strings.Repeat("b", 20) + "MATCH" +
						strings.Repeat("c", 600) + "MATCH" + strings.Repeat("d", 10),
				}},
			},
		},
		{
			name: "long-multiline",
			issue: detect.Issue{
				Type: detect.RegexIssue, Detail: "Multi-line rule", Match: "of\nmid\nthe", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{
					strings.Repeat("x", 600),
					strings.Repeat("a", 600) + "of",
					"mid",
					"the" + strings.Repeat("b", 600),
					"after",
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "report", tt.name+".txt"))
			if err != nil {
				t.Fatal(err)
			}

			var buf bytes.Buffer
			if err := (reporter{w: &buf}).issue(tt.issue, tt.meta); err != nil {
				t.Fatal(err)
			}

			if got := buf.String(); got != string(want) {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

func TestReporterColor(t *testing.T) {
	tests := []struct {
		name  string
		want  string
		issue detect.Issue
	}{
		{
			name: "single line",
			issue: detect.Issue{
				Type: detect.EntropyIssue, Match: "MATCH", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{"x MATCH y"}},
			},
			want: "~~~~~~~~~~~~~~~~~~~~~\n" +
				"\x1b[92mReason: High Entropy\x1b[0m\n" +
				"\x1b[92mFilepath: a.txt\x1b[0m\n" +
				"\x1b[92mSignature: sig\x1b[0m\n" +
				"+x \x1b[93mMATCH\x1b[0m y\n" +
				"~~~~~~~~~~~~~~~~~~~~~\n",
		},
		{
			name: "multiline",
			issue: detect.Issue{
				Type: detect.RegexIssue, Detail: "Rule", Match: "of\nthe", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{"start of", "the end"}},
			},
			want: "~~~~~~~~~~~~~~~~~~~~~\n" +
				"\x1b[92mReason: Regular Expression\x1b[0m\n" +
				"\x1b[92mDetail: Rule\x1b[0m\n" +
				"\x1b[92mFilepath: a.txt\x1b[0m\n" +
				"\x1b[92mSignature: sig\x1b[0m\n" +
				"+start \x1b[93mof\x1b[0m\n" +
				"+\x1b[93mthe\x1b[0m end\n" +
				"~~~~~~~~~~~~~~~~~~~~~\n",
		},
		{
			name: "long line",
			issue: detect.Issue{
				Type: detect.EntropyIssue, Match: "MATCH", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{
					strings.Repeat("a", 300) + "MATCH" + strings.Repeat("b", 300),
				}},
			},
			want: "~~~~~~~~~~~~~~~~~~~~~\n" +
				"\x1b[92mReason: High Entropy\x1b[0m\n" +
				"\x1b[92mFilepath: a.txt\x1b[0m\n" +
				"\x1b[92mSignature: sig\x1b[0m\n" +
				"+…" + strings.Repeat("a", 80) + "\x1b[93mMATCH\x1b[0m" + strings.Repeat("b", 80) + "…\n" +
				"~~~~~~~~~~~~~~~~~~~~~\n",
		},
		{
			name: "long multiline",
			issue: detect.Issue{
				Type: detect.RegexIssue, Detail: "Rule", Match: "of\nmid\nthe", Path: "a.txt", Signature: "sig",
				Chunk: detect.Chunk{Path: "a.txt", Lines: []string{
					strings.Repeat("a", 600) + "of", "mid", "the" + strings.Repeat("b", 600),
				}},
			},
			want: "~~~~~~~~~~~~~~~~~~~~~\n" +
				"\x1b[92mReason: Regular Expression\x1b[0m\n" +
				"\x1b[92mDetail: Rule\x1b[0m\n" +
				"\x1b[92mFilepath: a.txt\x1b[0m\n" +
				"\x1b[92mSignature: sig\x1b[0m\n" +
				"+…" + strings.Repeat("a", 80) + "\x1b[93mof\x1b[0m\n" +
				"+\x1b[93mmid\x1b[0m\n" +
				"+\x1b[93mthe\x1b[0m" + strings.Repeat("b", 80) + "…\n" +
				"~~~~~~~~~~~~~~~~~~~~~\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := (reporter{w: &buf, color: true}).issue(tt.issue, nil); err != nil {
				t.Fatal(err)
			}

			if got := buf.String(); got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

func TestExcerpt(t *testing.T) {
	long := strings.Repeat("a", 300) + "MATCH" + strings.Repeat("b", 300)
	mid := strings.Index(long, "MATCH")
	// "é" is two bytes and "日" three, so a byte-counted cut would differ.
	multibyte := strings.Repeat("é", 300) + "MATCH" + strings.Repeat("日", 300)
	multibyteMid := strings.Index(multibyte, "MATCH")
	huge := strings.Repeat("M", 600)

	tests := []struct {
		name       string
		line       string
		want       string
		start, end int
	}{
		{
			name: "max length",
			line: strings.Repeat("a", maxLineRunes), start: 0, end: 0,
			want: strings.Repeat("a", maxLineRunes),
		},
		{
			name: "empty anchor",
			line: long, start: 0, end: 0,
			want: strings.Repeat("a", 80) + "…",
		},
		{
			name: "match at start",
			line: "MATCH" + strings.Repeat("b", 600), start: 0, end: 5,
			want: "MATCH" + strings.Repeat("b", 80) + "…",
		},
		{
			name: "match in middle",
			line: long, start: mid, end: mid + 5,
			want: "…" + strings.Repeat("a", 80) + "MATCH" + strings.Repeat("b", 80) + "…",
		},
		{
			name: "match at end",
			line: strings.Repeat("a", 600) + "MATCH", start: 600, end: 605,
			want: "…" + strings.Repeat("a", 80) + "MATCH",
		},
		{
			name: "short context",
			line: strings.Repeat("a", 10) + "MATCH" + strings.Repeat("b", 600), start: 10, end: 15,
			want: strings.Repeat("a", 10) + "MATCH" + strings.Repeat("b", 80) + "…",
		},
		{
			name: "match longer than max",
			line: "a" + huge + "b", start: 1, end: 601,
			want: "a" + huge + "b",
		},
		{
			name: "match longer than max with context",
			line: strings.Repeat("a", 100) + huge, start: 100, end: 700,
			want: "…" + strings.Repeat("a", 80) + huge,
		},
		{
			name: "multi-byte runes",
			line: multibyte, start: multibyteMid, end: multibyteMid + 5,
			want: "…" + strings.Repeat("é", 80) + "MATCH" + strings.Repeat("日", 80) + "…",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := excerpt(tt.line, tt.start, tt.end)
			if got != tt.want {
				t.Errorf("excerpt() = %q, want %q", got, tt.want)
			}

			if !utf8.ValidString(got) {
				t.Errorf("excerpt() = %q is not valid UTF-8", got)
			}
		})
	}
}

// TestExcerptsLineBoundary pins the multi-line anchoring when the match
// starts or ends at a "\n": the line the match only touches must keep that
// edge, or the windowed body would lose the match and its highlight.
func TestExcerptsLineBoundary(t *testing.T) {
	lines := []string{strings.Repeat("a", 600) + "X", "mid", "Y" + strings.Repeat("b", 600)}
	// A segment anchor keeps contextRunes beside the segment; an empty anchor
	// at the line's edge keeps contextRunes including the edge rune.
	headSegment := "…" + strings.Repeat("a", 80) + "X"
	tailSegment := "Y" + strings.Repeat("b", 80) + "…"
	headEdge := "…" + strings.Repeat("a", 79) + "X"
	tailEdge := "Y" + strings.Repeat("b", 79) + "…"
	untouched := strings.Repeat("a", 80) + "…"

	tests := []struct {
		name  string
		match string
		want  []string
	}{
		{name: "inside lines", match: "X\nmid\nY", want: []string{headSegment, "mid", tailSegment}},
		{name: "starts at newline", match: "\nmid", want: []string{headEdge, "mid", tailEdge}},
		{name: "ends at newline", match: "mid\n", want: []string{untouched, "mid", tailEdge}},
		{name: "both at newline", match: "\nmid\n", want: []string{headEdge, "mid", tailEdge}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := excerpts(detect.Issue{Match: tt.match, Chunk: detect.Chunk{Lines: lines}})
			if strings.Join(got, "\n") != strings.Join(tt.want, "\n") {
				t.Errorf("excerpts() = %q, want %q", got, tt.want)
			}

			if !strings.Contains(strings.Join(got, "\n"), tt.match) {
				t.Errorf("excerpts() = %q lost the match %q", got, tt.match)
			}
		})
	}
}

func TestReporterSummary(t *testing.T) {
	tests := []struct {
		want string
		n    int
	}{
		{n: 0, want: "All clear. No secrets detected.\n"},
		{n: 1, want: "Found 1 issue(s).\n"},
		{n: 12, want: "Found 12 issue(s).\n"},
	}

	for _, tt := range tests {
		var buf bytes.Buffer
		if err := (reporter{w: &buf}).summary(tt.n); err != nil {
			t.Fatal(err)
		}

		if got := buf.String(); got != tt.want {
			t.Errorf("summary(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestUseColor(t *testing.T) {
	var buf bytes.Buffer
	if useColor(&Globals{}, &buf) {
		t.Error("useColor(buffer) = true")
	}

	pipe, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = pipe.Close()
		_ = w.Close()
	})

	if useColor(&Globals{}, w) {
		t.Error("useColor(pipe) = true")
	}

	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		t.Skipf("no terminal to test with: %v", err)
	}

	t.Cleanup(func() { _ = tty.Close() })

	t.Setenv("NO_COLOR", "")

	if !useColor(&Globals{}, tty) {
		t.Error("useColor(terminal) = false")
	}

	if useColor(&Globals{NoColor: true}, tty) {
		t.Error("useColor(terminal) with --no-color = true")
	}

	t.Setenv("NO_COLOR", "1")

	if useColor(&Globals{}, tty) {
		t.Error("useColor(terminal) with NO_COLOR=1 = true")
	}
}
