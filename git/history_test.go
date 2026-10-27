package git

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/satoseino/gelato/internal/gittest"
)

func commits(t *testing.T, r *Repo) []Commit {
	t.Helper()

	cs, err := r.Commits(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	return cs
}

func patches(t *testing.T, r *Repo, cs []Commit) []change {
	t.Helper()

	return collect(t, r.Patches(context.Background(), cs))
}

func head(repo *gittest.Repo, rev string) string {
	return strings.TrimSpace(repo.Git("rev-parse", rev))
}

func TestCommitsEmpty(t *testing.T) {
	repo := gittest.New(t)

	if cs := commits(t, open(t, repo.Dir)); len(cs) != 0 {
		t.Errorf("Commits() = %v, want none", cs)
	}
}

// TestCommitsRefs checks that commits reachable only from a remote-tracking
// ref, only from a detached HEAD, or only from a second root are listed, each
// once, children first.
func TestCommitsRefs(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "a\n")
	repo.Commit("root")
	root := head(repo, "HEAD")

	repo.Write("b.txt", "b\n")
	repo.Commit("remote only")
	remote := head(repo, "HEAD")
	repo.Git("update-ref", "refs/remotes/origin/x", remote)
	repo.Git("reset", "--quiet", "--hard", root)

	repo.Git("checkout", "--quiet", "--orphan", "other")
	repo.Write("c.txt", "c\n")
	repo.Commit("second root")
	other := head(repo, "HEAD")

	repo.Git("checkout", "--quiet", "--detach", root)
	repo.Write("d.txt", "d\n")
	repo.Commit("detached")
	detached := head(repo, "HEAD")

	got := map[string]string{}

	for _, c := range commits(t, open(t, repo.Dir)) {
		if _, ok := got[c.ID]; ok {
			t.Errorf("commit %s listed twice", c.ID)
		}

		got[c.ID] = c.FirstParent
	}

	want := map[string]string{root: "", remote: root, other: "", detached: root}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Commits() = %v, want %v", got, want)
	}
}

// TestPatchesMerge checks that a merge is diffed against its first parent,
// so that it re-shows the side branch's lines, and that each change carries
// its commit.
func TestPatchesMerge(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "a\n")
	repo.Commit("root")
	repo.Git("checkout", "--quiet", "-b", "side")
	repo.Write("side.txt", "side\n")
	repo.Commit("side")
	side := head(repo, "HEAD")
	repo.Git("checkout", "--quiet", "main")
	repo.Write("main.txt", "main\n")
	repo.Commit("main")
	repo.Git("merge", "--quiet", "--no-edit", "side")
	merge := head(repo, "HEAD")

	r := open(t, repo.Dir)
	cs := commits(t, r)

	if len(cs) != 4 || cs[0].ID != merge {
		t.Fatalf("Commits() = %v, want 4 commits starting with the merge %s", cs, merge)
	}

	var got []change

	for _, c := range patches(t, r, cs) {
		if c.Path == "side.txt" {
			got = append(got, c)
		}
	}

	want := []change{
		{Commit: merge, Path: "side.txt", Status: "Added", Lines: []string{"side"}},
		{Commit: side, Path: "side.txt", Status: "Added", Lines: []string{"side"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("side.txt changes = %v, want %v", got, want)
	}
}

// TestPatchesShallow checks that the boundary commit of a --depth 1 clone is
// diffed against the empty tree, so lines added in older commits are seen.
func TestPatchesShallow(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("old.txt", "old\n")
	repo.Commit("old")
	repo.Write("new.txt", "new\n")
	repo.Commit("new")

	clone := repo.Clone("--depth", "1")
	r := open(t, clone.Dir)
	cs := commits(t, r)

	want := []Commit{{ID: head(repo, "HEAD")}}
	if !reflect.DeepEqual(cs, want) {
		t.Fatalf("Commits() = %v, want %v", cs, want)
	}

	wantChanges := []change{
		{Commit: want[0].ID, Path: "new.txt", Status: "Added", Lines: []string{"new"}},
		{Commit: want[0].ID, Path: "old.txt", Status: "Added", Lines: []string{"old"}},
	}
	if got := patches(t, r, cs); !reflect.DeepEqual(got, wantChanges) {
		t.Errorf("Patches() = %v, want %v", got, wantChanges)
	}
}

// TestPatchesSkipped checks that submodule entries, binary files and
// deletions in history yield nothing.
func TestPatchesSkipped(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("gone.txt", "gone\n")
	repo.Commit("root")
	repo.Git("update-index", "--add", "--cacheinfo", "160000,"+head(repo, "HEAD")+",sub")
	repo.Git("commit", "--quiet", "--message", "submodule")
	repo.Write("blob.bin", "\x00\x01binary\n")
	repo.Git("rm", "--quiet", "gone.txt")
	repo.Commit("binary and deletion")

	r := open(t, repo.Dir)

	want := []change{{Commit: head(repo, "HEAD~2"), Path: "gone.txt", Status: "Added", Lines: []string{"gone"}}}
	if got := patches(t, r, commits(t, r)); !reflect.DeepEqual(got, want) {
		t.Errorf("Patches() = %v, want %v", got, want)
	}
}

func TestCommitInfo(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "a\n")
	repo.Commit("first subject\n\nbody")
	first := head(repo, "HEAD")
	repo.Write("b.txt", "b\n")
	repo.Commit("second")
	second := head(repo, "HEAD")

	// A non-UTC committer keeps their offset.
	t.Setenv("GIT_COMMITTER_DATE", "2020-01-01T09:30:00+05:30")
	repo.Write("c.txt", "c\n")
	repo.Commit("third")
	third := head(repo, "HEAD")

	infos, err := open(t, repo.Dir).CommitInfo(context.Background(), []string{first, second, third})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]CommitInfo{
		first:  {Hash: first, Time: "2020-01-01T00:00:00Z", Subject: "first subject"},
		second: {Hash: second, Time: "2020-01-01T00:00:00Z", Subject: "second"},
		third:  {Hash: third, Time: "2020-01-01T09:30:00+05:30", Subject: "third"},
	}
	if !reflect.DeepEqual(infos, want) {
		t.Errorf("CommitInfo() = %v, want %v", infos, want)
	}
}

// TestCommitTime checks that the time is formatted the same on every git
// version: %cI prints a zero offset as "+00:00" before git 2.45.
func TestCommitTime(t *testing.T) {
	tests := []struct {
		raw     string
		want    string
		wantErr bool
	}{
		{raw: "1577836800 +0000", want: "2020-01-01T00:00:00Z"},
		{raw: "1577836800 -0000", want: "2020-01-01T00:00:00Z"},
		{raw: "1577836800 +0530", want: "2020-01-01T05:30:00+05:30"},
		{raw: "1577836800 -0800", want: "2019-12-31T16:00:00-08:00"},
		{raw: "0 +1400", want: "1970-01-01T14:00:00+14:00"},
		{raw: "1577836800", wantErr: true},
		{raw: "x +0000", wantErr: true},
		{raw: "0 0000", wantErr: true},
		{raw: "0 +00:00", wantErr: true},
		{raw: "0 +-100", wantErr: true},
		{raw: "0 +0060", wantErr: true},
	}

	for _, tt := range tests {
		got, err := commitTime(tt.raw)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("commitTime(%q) = %q, %v; want %q, error %t", tt.raw, got, err, tt.want, tt.wantErr)
		}
	}
}
