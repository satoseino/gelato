package command

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

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
