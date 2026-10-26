package git

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/satoseino/gelato/internal/gittest"
)

func open(t *testing.T, dir string, opts ...Option) *Repo {
	t.Helper()

	r, err := Open(dir, opts...)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

func staged(t *testing.T, r *Repo) []change {
	t.Helper()

	return collect(t, r.Staged(context.Background()))
}

func TestOpenRoot(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("sub/dir/f.txt", "word\n")

	r := open(t, filepath.Join(repo.Dir, "sub", "dir"))
	if r.Root() != repo.Dir {
		t.Errorf("Root() = %q, want %q", r.Root(), repo.Dir)
	}
}

func TestOpenNotARepository(t *testing.T) {
	gittest.New(t)

	dir := t.TempDir()
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(dir))

	_, err := Open(dir)
	if err == nil || !strings.Contains(err.Error(), dir+" is not a valid git repository: fatal: not a git repository") {
		t.Errorf("got error %v", err)
	}
}

func TestStagedUnborn(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("notes.txt", "first line\nsecond line\n")
	repo.Git("add", "notes.txt")

	want := []change{{Path: "notes.txt", Status: "Added", Lines: []string{"first line", "second line"}}}
	if got := staged(t, open(t, repo.Dir)); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStagedRenameWithEdit(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("old.txt", "alpha beta\ngamma delta\nepsilon zeta\neta theta\n")
	repo.Commit("init")
	repo.Git("mv", "old.txt", "new.txt")
	repo.Write("new.txt", "alpha beta\ngamma delta\nepsilon zeta\neta theta\niota kappa\n")
	repo.Git("add", "new.txt")

	want := []change{{Path: "new.txt", Status: "Renamed", Lines: []string{"iota kappa"}}}
	if got := staged(t, open(t, repo.Dir)); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestStagedEmptyFileWithSpace(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("e mpty b", "")
	repo.Git("add", "e mpty b")

	want := []change{{Path: "e mpty b", Status: "Added", Lines: []string{}}}
	if got := staged(t, open(t, repo.Dir)); !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestStagedHostileConfig stages every kind of change, checks what Staged
// returns, then sets repo config that would change the patch format if it
// weren't pinned and checks that the result is unchanged.
func TestStagedHostileConfig(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("mod.txt", "one\ntwo\n")
	repo.Write("del.txt", "gone\n")
	repo.Write("bin.dat", "\x00\x01")
	repo.Write("sub/old.txt", "alpha beta\ngamma delta\nepsilon zeta\neta theta\n")
	repo.Commit("init")

	first := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
	repo.Git("update-index", "--add", "--cacheinfo", "160000,"+first+",mysub")
	repo.Git("commit", "--quiet", "--message", "add gitlink")

	second := strings.TrimSpace(repo.Git("rev-parse", "HEAD"))
	repo.Git("update-index", "--cacheinfo", "160000,"+second+",mysub")

	tricky := `sp ace "quote" ü.txt`

	repo.Write("mod.txt", "one\ntwo\nthree\n")
	repo.Write("bin.dat", "\x00\x02")
	repo.Write(tricky, "word\n")
	repo.Git("rm", "--quiet", "del.txt")
	repo.Git("mv", "sub/old.txt", "sub/new.txt")
	repo.Write("sub/new.txt", "alpha beta\ngamma delta\nepsilon zeta\neta theta\niota kappa\n")
	repo.Git("add", "mod.txt", "bin.dat", tricky, "sub/new.txt")

	want := []change{
		{Path: "mod.txt", Status: "Modified", Lines: []string{"three"}},
		{Path: tricky, Status: "Added", Lines: []string{"word"}},
		{Path: "sub/new.txt", Status: "Renamed", Lines: []string{"iota kappa"}},
	}
	if got := staged(t, open(t, repo.Dir)); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}

	for _, kv := range [][2]string{
		{"diff.noprefix", "true"},
		{"diff.mnemonicPrefix", "true"},
		{"diff.submodule", "log"},
		{"diff.relative", "true"},
		{"diff.algorithm", "patience"},
		{"diff.renames", "false"},
		{"color.ui", "always"},
		{"color.diff", "always"},
		{"core.quotePath", "true"},
		{"diff.external", "false"},
	} {
		repo.Git("config", kv[0], kv[1])
	}

	if got := staged(t, open(t, filepath.Join(repo.Dir, "sub"))); !reflect.DeepEqual(got, want) {
		t.Errorf("with hostile config: got %q, want %q", got, want)
	}
}

// TestStagedIndexFile builds an alternate index, as git commit -a does for
// its hooks, and checks that GIT_INDEX_FILE selects it by default but not
// under WithoutRepoEnv.
func TestStagedIndexFile(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "one\n")
	repo.Commit("init")
	repo.Write("a.txt", "one\ntwo\n")

	alt := filepath.Join(t.TempDir(), "index.alt")
	repo.Git("read-tree", "--index-output="+alt, "HEAD")
	t.Setenv("GIT_INDEX_FILE", alt)
	repo.Git("add", "a.txt")

	want := []change{{Path: "a.txt", Status: "Modified", Lines: []string{"two"}}}
	if got := staged(t, open(t, repo.Dir)); !reflect.DeepEqual(got, want) {
		t.Errorf("default: got %q, want %q", got, want)
	}

	if got := staged(t, open(t, repo.Dir, WithoutRepoEnv())); len(got) != 0 {
		t.Errorf("WithoutRepoEnv: got %q, want nothing", got)
	}
}

func TestStagedGitFailure(t *testing.T) {
	repo := gittest.New(t)
	r := open(t, repo.Dir)

	var got error
	for _, err := range r.patches(context.Background(), nil, "diff", "no-such-revision") {
		got = err
	}

	if got == nil || !strings.HasPrefix(got.Error(), "git diff failed: exit status 128: fatal: ") {
		t.Errorf("got error %v", got)
	}
}

func TestStagedStopsEarly(t *testing.T) {
	repo := gittest.New(t)
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		repo.Write(name, "word\n")
	}

	repo.Git("add", "--all")

	n := 0
	for _, err := range open(t, repo.Dir).Staged(context.Background()) {
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

func TestStagedCanceled(t *testing.T) {
	repo := gittest.New(t)
	r := open(t, repo.Dir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var got error
	for _, err := range r.Staged(ctx) {
		got = err
	}

	if got == nil {
		t.Error("no error from a canceled context")
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		out  string
		want [3]int
		err  bool
	}{
		{out: "git version 2.56.0\n", want: [3]int{2, 56, 0}},
		{out: "git version 2.39.3 (Apple Git-146)\n", want: [3]int{2, 39, 3}},
		{out: "git version 2.45.1.windows.1\n", want: [3]int{2, 45, 1}},
		{out: "git version 2.50.0-rc1\n", want: [3]int{2, 50, 0}},
		{out: "git version 2.30\n", want: [3]int{2, 30, 0}},
		{out: "git version 2\n", err: true},
		{out: "git version x.y.z\n", err: true},
		{out: "hub version 2.30.0\n", err: true},
		{out: "", err: true},
	}

	for _, tt := range tests {
		got, err := parseVersion(tt.out)
		if (err != nil) != tt.err || (!tt.err && got != tt.want) {
			t.Errorf("parseVersion(%q) = %v, %v; want %v, error %t", tt.out, got, err, tt.want, tt.err)
		}
	}
}

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		out string
		err string
	}{
		{out: "git version 2.30.0"},
		{out: "git version 3.0.0"},
		{out: "git version 2.29.9", err: "git 2.29.9 is too old: gelato requires git 2.30.0 or newer"},
		{out: "git version 1.99.0", err: "git 1.99.0 is too old: gelato requires git 2.30.0 or newer"},
	}

	for _, tt := range tests {
		err := checkVersion(tt.out)

		got := ""
		if err != nil {
			got = err.Error()
		}

		if got != tt.err {
			t.Errorf("checkVersion(%q) = %q, want %q", tt.out, got, tt.err)
		}
	}
}
