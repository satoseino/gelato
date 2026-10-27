package command

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"

	"github.com/satoseino/gelato/detect"
	"github.com/satoseino/gelato/internal/gittest"
)

// fixture holds the values a testdata/precommit template can refer to.
type fixture struct {
	KeyID   string
	Entropy string
}

// Secrets, assembled at runtime so that no token-shaped literal is ever
// committed: one would be flagged by GitHub push protection and by
// gelato's own hook.
var (
	upper  = runeRange('A', 'Z')
	alnum  = upper + runeRange('a', 'z') + runeRange('0', '9')
	base32 = upper + runeRange('2', '7')

	// keyID matches the "AWS access key ID" rule and is too repetitive to
	// count as high entropy.
	keyID = "AK" + "IA" + base32[:16]

	// highEntropy is a high-entropy base64 string that matches no rule.
	highEntropy = alnum[:40]

	// testdata is the absolute path of testdata/, set by TestMain because
	// the tests change directory into their repositories.
	testdata string
)

func TestMain(m *testing.M) {
	var err error

	testdata, err = filepath.Abs("testdata")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)

		os.Exit(2)
	}

	os.Exit(m.Run())
}

// buildGelato builds the gelato binary into a temporary directory and
// returns its path. Only the real-hook test needs it, so a failed build
// fails that test alone.
func buildGelato(t *testing.T) string {
	t.Helper()

	bin := filepath.Join(t.TempDir(), "gelato")

	build := exec.Command("go", "build", "-o", bin, "github.com/satoseino/gelato/cmd/gelato")

	build.Env = append(os.Environ(), "CGO_ENABLED=0")

	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building gelato: %v\n%s", err, out)
	}

	return bin
}

func runeRange(lo, hi rune) string {
	var b strings.Builder
	for r := lo; r <= hi; r++ {
		b.WriteRune(r)
	}

	return b.String()
}

// preCommit runs the pre-commit command in dir as kong would and returns its
// output.
func preCommit(t *testing.T, dir string, g *Globals) (string, error) {
	t.Helper()
	t.Chdir(dir)

	var out bytes.Buffer

	c := &PreCommit{}
	if err := c.AfterApply(g, &out); err != nil {
		t.Fatal(err)
	}

	err := c.Run()

	return out.String(), err
}

// golden renders testdata/precommit/<name>.txt.tmpl with the fixture
// secrets.
func golden(t *testing.T, name string) string {
	t.Helper()

	return render(t, filepath.Join("precommit", name+".txt.tmpl"), fixture{KeyID: keyID, Entropy: highEntropy})
}

// render executes the template at path, relative to testdata/, with data. Its
// sig function computes a finding's signature.
func render(t *testing.T, path string, data any) string {
	t.Helper()

	tmpl, err := template.New(filepath.Base(path)).
		Funcs(template.FuncMap{"sig": detect.Signature}).
		ParseFiles(filepath.Join(testdata, path))
	if err != nil {
		t.Fatal(err)
	}

	var b strings.Builder
	if err := tmpl.Execute(&b, data); err != nil {
		t.Fatal(err)
	}

	return b.String()
}

func assertFindings(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ErrFindings) {
		t.Fatalf("got error %v, want ErrFindings", err)
	}
}

func assertClear(t *testing.T, out string, err error) {
	t.Helper()

	if err != nil {
		t.Fatalf("got error %v; output:\n%s", err, out)
	}

	if want := "All clear. No secrets detected.\n"; out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
}

func TestPreCommitClean(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "one\n")
	repo.Commit("init")
	repo.Write("a.txt", "one\ntwo\n")
	repo.Git("add", "a.txt")

	out, err := preCommit(t, repo.Dir, &Globals{})
	assertClear(t, out, err)
}

func TestPreCommitFindings(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("README", "readme\n")
	repo.Commit("init")
	repo.Write("config.py", "import os\naws_access_key_id = "+keyID+"\n")
	repo.Write("notes.txt", "value: "+highEntropy+"\n")
	repo.Git("add", "--all")

	out, err := preCommit(t, repo.Dir, &Globals{})
	assertFindings(t, err)

	if want := golden(t, "findings"); out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}

	if strings.Contains(out, "\x1b") {
		t.Error("output to a buffer contains ANSI escapes")
	}
}

func TestPreCommitExcluded(t *testing.T) {
	tests := []struct {
		name   string
		config string
	}{
		{
			name:   "raw string in exclude-signatures",
			config: "exclude-signatures = [{signature = '" + keyID + "', reason = 'test'}]",
		},
		{
			name:   "signature in exclude-signatures",
			config: "exclude-signatures = [{signature = '" + detect.Signature(keyID, "config.py") + "', reason = 'test'}]",
		},
		{
			name:   "exclude-path-patterns",
			config: `exclude-path-patterns = [{path-pattern = 'config\.py$', reason = 'test'}]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := gittest.New(t)
			// tartufo.toml is left unstaged, so only config.py is scanned.
			repo.Write("tartufo.toml", "[tool.tartufo]\n"+tt.config+"\n")
			repo.Write("config.py", "aws_access_key_id = "+keyID+"\n")
			repo.Git("add", "config.py")

			out, err := preCommit(t, repo.Dir, &Globals{})
			assertClear(t, out, err)
		})
	}
}

func TestPreCommitFileName(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("keys/"+keyID+".txt", "nothing to see\n")
	repo.Git("add", "--all")

	out, err := preCommit(t, repo.Dir, &Globals{})
	assertFindings(t, err)

	if want := golden(t, "file-name"); out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
}

func TestPreCommitOnlyAddedLines(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("kept.py", "aws_access_key_id = "+keyID+"\n")
	repo.Write("removed.py", "aws_access_key_id = "+keyID+"\n")
	repo.Commit("init")
	repo.Write("kept.py", "aws_access_key_id = "+keyID+"\nclean = 1\n")
	repo.Write("removed.py", "clean = 1\n")
	repo.Git("add", "--all")

	out, err := preCommit(t, repo.Dir, &Globals{})
	assertClear(t, out, err)
}

func TestPreCommitUnborn(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "one\n")
	repo.Git("add", "a.txt")

	out, err := preCommit(t, repo.Dir, &Globals{})
	assertClear(t, out, err)

	repo.Write("config.py", "aws_access_key_id = "+keyID+"\n")
	repo.Git("add", "config.py")

	out, err = preCommit(t, repo.Dir, &Globals{})
	assertFindings(t, err)

	if !strings.Contains(out, "Filepath: config.py\n") {
		t.Errorf("output:\n%s\nwant a finding in config.py", out)
	}
}

// TestPreCommitRename checks that a staged rename is scanned as an edit: the
// secret the file already held is not reported, the one the edit adds is.
func TestPreCommitRename(t *testing.T) {
	repo := gittest.New(t)

	old := "aws_access_key_id = " + keyID + "\n" + strings.Repeat("filler line\n", 10)
	repo.Write("old.py", old)
	repo.Commit("init")
	repo.Git("mv", "old.py", "new.py")
	repo.Write("new.py", old+"value: "+highEntropy+"\n")
	repo.Git("add", "new.py")

	out, err := preCommit(t, repo.Dir, &Globals{})
	assertFindings(t, err)

	if want := golden(t, "rename"); out != want {
		t.Errorf("output:\n%s\nwant:\n%s", out, want)
	}
}

func TestPreCommitDeterministic(t *testing.T) {
	repo := gittest.New(t)

	const files = 50
	for i := range files {
		rotated := alnum[i%len(alnum):] + alnum[:i%len(alnum)]
		repo.Write(fmt.Sprintf("dir%d/f%02d.txt", i%3, i), "value: "+rotated[:40]+"\nother line\n")
	}

	repo.Git("add", "--all")

	first, err := preCommit(t, repo.Dir, &Globals{})
	assertFindings(t, err)

	if want := fmt.Sprintf("Found %d issue(s).\n", files); !strings.HasSuffix(first, want) {
		t.Fatalf("output ends %q, want %q", first[max(0, len(first)-40):], want)
	}

	for range 5 {
		again, err := preCommit(t, repo.Dir, &Globals{})
		assertFindings(t, err)

		if again != first {
			t.Fatalf("output differs between runs:\n%s\nvs\n%s", first, again)
		}
	}
}

// TestPreCommitHook commits through an installed hook with git commit -a,
// which stages the working tree into a temporary index that it hands to the
// hook in GIT_INDEX_FILE. The index in .git/index never sees the secret, so
// the commit is only rejected if gelato scans the temporary one.
func TestPreCommitHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the hook is a shell script")
	}

	gelatoBin := buildGelato(t)

	repo := gittest.New(t)
	repo.Write("config.py", "import os\n")
	repo.Commit("init")

	head := repo.Git("rev-parse", "HEAD")

	hook := filepath.Join(repo.Dir, ".git", "hooks", "pre-commit")
	if err := os.MkdirAll(filepath.Dir(hook), 0o750); err != nil {
		t.Fatal(err)
	}

	script := "#!/bin/sh\nexec '" + gelatoBin + "' pre-commit\n"
	if err := os.WriteFile(hook, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(hook, 0o700); err != nil {
		t.Fatal(err)
	}

	repo.Write("config.py", "import os\naws_access_key_id = "+keyID+"\n")

	commit := exec.Command("git", "commit", "--all", "--message", "add key")
	commit.Dir = repo.Dir

	out, err := commit.CombinedOutput()
	if err == nil {
		t.Fatalf("git commit -a succeeded; output:\n%s", out)
	}

	for _, want := range []string{"Detail: AWS access key ID\n", "Filepath: config.py\n", "Found 1 issue(s).\n"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output:\n%s\nwant it to contain %q", out, want)
		}
	}

	if got := repo.Git("rev-parse", "HEAD"); got != head {
		t.Errorf("HEAD moved from %s to %s", head, got)
	}
}
