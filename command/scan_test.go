package command

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/satoseino/gelato/detect"
	"github.com/satoseino/gelato/internal/gittest"
)

// scanFixture holds the values a testdata/scan template can refer to.
type scanFixture struct {
	KeyID   string
	Entropy string
	Commit  string
}

// scan runs the scan command in dir as kong would, with repoPath as its
// argument, and returns its output.
func scan(t *testing.T, dir, repoPath string, g *Globals) (string, error) {
	t.Helper()
	t.Chdir(dir)

	var out bytes.Buffer

	c := &Scan{RepoPath: repoPath}
	if err := c.AfterApply(g, &out); err != nil {
		t.Fatal(err)
	}

	err := c.Run()

	return out.String(), err
}

func rev(repo *gittest.Repo, name string) string {
	return strings.TrimSpace(repo.Git("rev-parse", name))
}

// assertReported checks that out reports exactly the paths, in order, and
// that each is attributed to its commit.
func assertReported(t *testing.T, out string, want ...[2]string) {
	t.Helper()

	var got [][2]string

	var path string

	for line := range strings.Lines(out) {
		if p, ok := strings.CutPrefix(line, "Filepath: "); ok {
			path = strings.TrimSpace(p)
		}

		if h, ok := strings.CutPrefix(line, "Commit hash: "); ok {
			got = append(got, [2]string{path, strings.TrimSpace(h)})
		}
	}

	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("reported (path, commit) = %v, want %v; output:\n%s", got, want, out)
	}
}

// TestScanAddedThenRemoved checks that a secret is reported once, with the
// metadata of the commit that added it, even after it is removed.
func TestScanAddedThenRemoved(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("README", "readme\n")
	repo.Commit("init")
	repo.Write("config.py", "aws_access_key_id = "+keyID+"\n")
	repo.Write("notes.txt", "value: "+highEntropy+"\n")
	repo.Commit("add key")
	added := rev(repo, "HEAD")
	repo.Write("README", "readme\nmore\n")
	repo.Commit("unrelated")
	repo.Write("config.py", "clean = 1\n")
	repo.Git("rm", "--quiet", "notes.txt")
	repo.Commit("remove key")

	out, err := scan(t, repo.Dir, "", &Globals{})
	assertFindings(t, err)

	want := render(t, filepath.Join("scan", "added-removed.txt.tmpl"),
		scanFixture{KeyID: keyID, Entropy: highEntropy, Commit: added})
	if out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
}

// TestScanMerge checks that a secret added on a side branch is reported once,
// at the side-branch commit, although the merge re-shows it.
func TestScanMerge(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("README", "readme\n")
	repo.Commit("init")
	repo.Git("checkout", "--quiet", "-b", "side")
	repo.Write("config.py", "aws_access_key_id = "+keyID+"\n")
	repo.Commit("side")
	side := rev(repo, "HEAD")
	repo.Git("checkout", "--quiet", "main")
	repo.Write("main.txt", "main\n")
	repo.Commit("main")
	repo.Git("merge", "--quiet", "--no-edit", "side")
	repo.Git("branch", "--quiet", "-D", "side")

	out, err := scan(t, repo.Dir, "", &Globals{})
	assertFindings(t, err)
	assertReported(t, out, [2]string{"config.py", side})
}

// TestScanRefs checks that secrets only reachable from a remote-tracking
// ref, from a detached HEAD, or from either of two roots are all reported.
func TestScanRefs(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("root1.py", "aws_access_key_id = "+keyID+"\n")
	repo.Commit("root 1")
	root1 := rev(repo, "HEAD")

	repo.Write("remote.py", "aws_access_key_id = "+keyID+"\n")
	repo.Commit("remote")
	remote := rev(repo, "HEAD")
	repo.Git("update-ref", "refs/remotes/origin/x", remote)
	repo.Git("reset", "--quiet", "--hard", root1)

	repo.Git("checkout", "--quiet", "--orphan", "other")
	repo.Git("rm", "--quiet", "-r", "--cached", ".")
	repo.Write("root2.py", "aws_access_key_id = "+keyID+"\n")
	repo.Git("add", "root2.py")
	repo.Git("commit", "--quiet", "--message", "root 2")
	root2 := rev(repo, "HEAD")

	repo.Git("checkout", "--quiet", "--force", "--detach", root1)
	repo.Write("detached.py", "aws_access_key_id = "+keyID+"\n")
	repo.Commit("detached")
	detached := rev(repo, "HEAD")

	out, err := scan(t, repo.Dir, "", &Globals{})
	assertFindings(t, err)

	got := map[string]string{}

	for line := range strings.Lines(out) {
		if h, ok := strings.CutPrefix(line, "Commit hash: "); ok {
			got[strings.TrimSpace(h)] = ""
		}
	}

	for name, id := range map[string]string{"root 1": root1, "remote": remote, "root 2": root2, "detached": detached} {
		if _, ok := got[id]; !ok {
			t.Errorf("no finding in commit %q; output:\n%s", name, out)
		}
	}

	if !strings.HasSuffix(out, "Found 4 issue(s).\n") {
		t.Errorf("output:\n%s\nwant 4 issues", out)
	}
}

// TestScanShallow checks that in a --depth 1 clone a secret added before the
// boundary is reported, at the boundary commit.
func TestScanShallow(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("config.py", "aws_access_key_id = "+keyID+"\n")
	repo.Commit("old")
	repo.Write("README", "readme\n")
	repo.Commit("new")

	clone := repo.Clone("--depth", "1")

	out, err := scan(t, clone.Dir, "", &Globals{})
	assertFindings(t, err)
	assertReported(t, out, [2]string{"config.py", rev(repo, "HEAD")})
}

func TestScanEmpty(t *testing.T) {
	repo := gittest.New(t)

	out, err := scan(t, repo.Dir, "", &Globals{})
	assertClear(t, out, err)
}

// TestScanSkipped checks that submodule entries and binary files in history
// are not scanned.
func TestScanSkipped(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("README", "readme\n")
	repo.Commit("init")
	repo.Git("update-index", "--add", "--cacheinfo", "160000,"+rev(repo, "HEAD")+","+keyID)
	repo.Git("commit", "--quiet", "--message", "submodule")
	repo.Write("blob.bin", "\x00aws_access_key_id = "+keyID+"\n")
	repo.Commit("binary")

	out, err := scan(t, repo.Dir, "", &Globals{})
	assertClear(t, out, err)
}

// TestScanRepoPath checks where the repository comes from: the argument, else
// repo-path from --config, else the current directory, and that an inherited
// GIT_DIR never redirects the scan.
func TestScanRepoPath(t *testing.T) {
	dirty := gittest.New(t)
	dirty.Write("config.py", "aws_access_key_id = "+keyID+"\n")
	dirty.Commit("add key")
	dirtyHead := rev(dirty, "HEAD")

	clean := gittest.New(t)
	clean.Write("README", "readme\n")
	clean.Commit("init")

	config := filepath.Join(t.TempDir(), "gelato.toml")
	if err := os.WriteFile(config, []byte("[tool.tartufo]\nrepo-path = '"+filepath.ToSlash(clean.Dir)+"'\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		cwd      string
		repoPath string
		config   []string
		dirty    bool
	}{
		{name: "argument", cwd: clean.Dir, repoPath: dirty.Dir, dirty: true},
		{name: "argument over config", cwd: clean.Dir, repoPath: dirty.Dir, config: []string{config}, dirty: true},
		{name: "config", cwd: dirty.Dir, config: []string{config}},
		{name: "current directory", cwd: dirty.Dir, dirty: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Point GIT_DIR at the other repository.
			other := dirty
			if tt.dirty {
				other = clean
			}

			t.Setenv("GIT_DIR", filepath.Join(other.Dir, ".git"))

			out, err := scan(t, tt.cwd, tt.repoPath, &Globals{Config: tt.config})
			if tt.dirty {
				assertFindings(t, err)
				assertReported(t, out, [2]string{"config.py", dirtyHead})
			} else {
				assertClear(t, out, err)
			}
		})
	}
}

// TestScanDeterministic checks that output is byte-identical across runs and
// across batch counts, including ones that don't divide the history evenly,
// and that a merge's side-branch secret stays attributed to the side commit
// wherever the batch boundaries fall.
func TestScanDeterministic(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(0))

	repo := gittest.New(t)

	var oldest string

	const commits = 30
	for i := range commits {
		rotated := alnum[i%len(alnum):] + alnum[:i%len(alnum)]
		repo.Write(fmt.Sprintf("dir%d/f%02d.txt", i%3, i), "value: "+rotated[:40]+"\nother line\n")
		// Every other commit adds the key again; it is reported only once.
		if i%2 == 0 {
			repo.Write("config.py", "aws_access_key_id = "+keyID+"\n")
		} else {
			repo.Write("config.py", "clean = 1\n")
		}

		repo.Commit(fmt.Sprintf("commit %d", i))

		if i == 0 {
			oldest = rev(repo, "HEAD")
		}
	}

	repo.Git("checkout", "--quiet", "-b", "side", "HEAD~20")
	repo.Write("side.py", "aws_access_key_id = "+keyID+"\n")
	repo.Commit("side")
	side := rev(repo, "HEAD")
	repo.Git("checkout", "--quiet", "main")
	repo.Git("merge", "--quiet", "--no-edit", "side")

	runtime.GOMAXPROCS(1)

	first, err := scan(t, repo.Dir, "", &Globals{})
	assertFindings(t, err)

	if want := fmt.Sprintf("Found %d issue(s).\n", commits+2); !strings.HasSuffix(first, want) {
		t.Fatalf("output ends %q, want %q", first[max(0, len(first)-40):], want)
	}

	for path, id := range map[string]string{"config.py": oldest, "side.py": side} {
		if want := "Filepath: " + path + "\nSignature: " + detect.Signature(keyID, path) + "\nCommit hash: " + id + "\n"; !strings.Contains(first, want) {
			t.Errorf("output:\n%s\nwant the key in %s attributed to %s", first, path, id)
		}
	}

	for _, procs := range []int{4, 4, 7, 32} {
		runtime.GOMAXPROCS(procs)

		again, err := scan(t, repo.Dir, "", &Globals{})
		assertFindings(t, err)

		if again != first {
			t.Fatalf("output with GOMAXPROCS %d differs from GOMAXPROCS 1:\n%s\nvs\n%s", procs, again, first)
		}
	}
}
