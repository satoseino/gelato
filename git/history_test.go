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

	infos, err := open(t, repo.Dir).CommitInfo(context.Background(), []string{first, second})
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]CommitInfo{
		first:  {Hash: first, Time: "2020-01-01T00:00:00Z", Subject: "first subject"},
		second: {Hash: second, Time: "2020-01-01T00:00:00Z", Subject: "second"},
	}
	if !reflect.DeepEqual(infos, want) {
		t.Errorf("CommitInfo() = %v, want %v", infos, want)
	}
}
