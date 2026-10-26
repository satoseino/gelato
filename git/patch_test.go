package git

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
)

// change is a JSON-friendly view of a FileChange, with Status by name.
type change struct {
	Commit string   `json:"commit"`
	Path   string   `json:"path"`
	Status string   `json:"status"`
	Lines  []string `json:"lines"`
}

// testdata holds the captured patches and their expected results.
var testdata = os.DirFS("testdata")

// fixture returns a reader over the testdata file name.
func fixture(t *testing.T, name string) *bytes.Reader {
	t.Helper()

	data, err := fs.ReadFile(testdata, name)
	if err != nil {
		t.Fatal(err)
	}

	return bytes.NewReader(data)
}

func collect(t *testing.T, seq func(func(FileChange, error) bool)) []change {
	t.Helper()

	out := []change{}

	for fc, err := range seq {
		if err != nil {
			t.Fatal(err)
		}

		lines := fc.Lines
		if lines == nil {
			lines = []string{}
		}

		out = append(out, change{Commit: fc.Commit, Path: fc.Path, Status: fc.Status.String(), Lines: lines})
	}

	return out
}

// TestParsePatch parses each testdata/*.patch, captured from real git with
// the pinned options, and compares it with the matching .json.
func TestParsePatch(t *testing.T) {
	patches, err := fs.Glob(testdata, "*.patch")
	if err != nil {
		t.Fatal(err)
	}

	if len(patches) == 0 {
		t.Fatal("no patches")
	}

	for _, patch := range patches {
		name := strings.TrimSuffix(patch, ".patch")
		t.Run(name, func(t *testing.T) {
			data, err := fs.ReadFile(testdata, name+".json")
			if err != nil {
				t.Fatal(err)
			}

			var want []change
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}

			if got := collect(t, ParsePatch(fixture(t, patch))); !reflect.DeepEqual(got, want) {
				t.Errorf("got  %q\nwant %q", got, want)
			}
		})
	}
}

// TestParsePatchInvalid checks that each testdata/invalid/*.patch fails with
// the error in the matching .err.
func TestParsePatchInvalid(t *testing.T) {
	patches, err := fs.Glob(testdata, "invalid/*.patch")
	if err != nil {
		t.Fatal(err)
	}

	if len(patches) == 0 {
		t.Fatal("no patches")
	}

	for _, patch := range patches {
		name := strings.TrimSuffix(patch, ".patch")
		t.Run(path.Base(name), func(t *testing.T) {
			want, err := fs.ReadFile(testdata, name+".err")
			if err != nil {
				t.Fatal(err)
			}

			var got error
			for _, err := range ParsePatch(fixture(t, patch)) {
				got = err
			}

			if got == nil || got.Error() != strings.TrimSuffix(string(want), "\n") {
				t.Errorf("got error %v, want %s", got, want)
			}
		})
	}
}

func TestParsePatchLongLine(t *testing.T) {
	long := strings.Repeat("word ", 1<<20)
	patch := "diff --git a/min.js b/min.js\nnew file mode 100644\nindex 0000000..1111111\n" +
		"--- /dev/null\n+++ b/min.js\n@@ -0,0 +1 @@\n+" + long + "\n\\ No newline at end of file"

	got := collect(t, ParsePatch(strings.NewReader(patch)))
	if len(got) != 1 || len(got[0].Lines) != 1 || got[0].Lines[0] != long {
		t.Errorf("long line not returned intact")
	}
}

func TestParsePatchStopsEarly(t *testing.T) {
	n := 0
	for _, err := range ParsePatch(fixture(t, "staged.patch")) {
		if err != nil {
			t.Fatal(err)
		}

		n++

		break
	}

	if n != 1 {
		t.Errorf("yielded %d changes after break, want 1", n)
	}
}

func TestGitHeaderPath(t *testing.T) {
	tests := []struct {
		header string
		want   string
		err    bool
	}{
		{header: "a/x b/x", want: "x"},
		{header: "a/e mpty b b/e mpty b", want: "e mpty b"},
		{header: "a/ b/ b/ b/", want: " b/"},
		{header: `"a/em\tpty" "b/em\tpty"`, want: "em\tpty"},
		{header: `"a/\303\274" "b/\303\274"`, want: "ü"},
		{header: "a/x b/y", err: true},
		{header: "a/xy b/x", err: true},
		{header: "x/x y/x", err: true},
		{header: `"a/x" "b/y"`, err: true},
		{header: `"a/x`, err: true},
	}

	for _, tt := range tests {
		got, err := gitHeaderPath(tt.header)
		if (err != nil) != tt.err || got != tt.want {
			t.Errorf("gitHeaderPath(%q) = %q, %v; want %q, error %t", tt.header, got, err, tt.want, tt.err)
		}
	}
}

func TestParseHunkHeader(t *testing.T) {
	tests := []struct {
		header   string
		old, new int
		err      bool
	}{
		{header: "@@ -2 +2 @@", old: 1, new: 1},
		{header: "@@ -3,0 +4 @@ three", old: 0, new: 1},
		{header: "@@ -0,0 +1,2 @@", old: 0, new: 2},
		{header: "@@ -1,5 +0,0 @@ func f() { @@ x", old: 5, new: 0},
		{header: "@@ -1 +1", err: true},
		{header: "@@ 1 +1 @@", err: true},
		{header: "@@ -1 +1,x @@", err: true},
		{header: "@@ -1,-1 +1 @@", err: true},
	}

	for _, tt := range tests {
		oldCount, newCount, err := parseHunkHeader(tt.header)
		if (err != nil) != tt.err || oldCount != tt.old || newCount != tt.new {
			t.Errorf("parseHunkHeader(%q) = %d, %d, %v; want %d, %d, error %t",
				tt.header, oldCount, newCount, err, tt.old, tt.new, tt.err)
		}
	}
}

func TestIsObjectID(t *testing.T) {
	tests := map[string]bool{
		strings.Repeat("0123456789", 4):                  true,
		strings.Repeat("abcdef0123456789", 4):            true,
		strings.Repeat("0123456789", 4)[:39]:             false,
		strings.Repeat("ABCDEF0123", 4):                  false,
		strings.Repeat("0123456789", 4) + " " + "parent": false,
		"diff --git a/x b/x":                             false,
	}

	for line, want := range tests {
		if got := isObjectID(line); got != want {
			t.Errorf("isObjectID(%q) = %t, want %t", line, got, want)
		}
	}
}

func TestStatusString(t *testing.T) {
	for s, want := range map[Status]string{Modified: "Modified", Added: "Added", Renamed: "Renamed", 7: "Status(7)"} {
		if got := s.String(); got != want {
			t.Errorf("Status(%d).String() = %q, want %q", int(s), got, want)
		}
	}
}
