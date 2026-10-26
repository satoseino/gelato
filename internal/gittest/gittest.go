// Package gittest builds temporary git repositories for tests with the real
// git binary, isolated from the user's and the system's git config.
package gittest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Repo is a temporary repository with a working tree.
type Repo struct {
	t   testing.TB
	Dir string
}

var (
	// isolatedEnv makes every git process in the test, including the code
	// under test, ignore user and system config and commit with a fixed
	// identity and date, so object IDs are reproducible.
	isolatedEnv = map[string]string{
		"GIT_CONFIG_GLOBAL":   os.DevNull,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_AUTHOR_NAME":     "Gelato Test",
		"GIT_AUTHOR_EMAIL":    "test@example.com",
		"GIT_AUTHOR_DATE":     "2020-01-01T00:00:00Z",
		"GIT_COMMITTER_NAME":  "Gelato Test",
		"GIT_COMMITTER_EMAIL": "test@example.com",
		"GIT_COMMITTER_DATE":  "2020-01-01T00:00:00Z",
	}

	// inheritedRepoEnv lists variables a surrounding hook (e.g. go test run
	// from a pre-commit hook) could set that would redirect git away from the
	// test repository.
	inheritedRepoEnv = []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_PREFIX",
		"GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	}
)

// New isolates the test process's git environment and initializes an empty
// repository on branch main in a new temporary directory. It changes the
// process environment, so tests that use it can't run in parallel.
func New(tb testing.TB) *Repo {
	tb.Helper()

	for k, v := range isolatedEnv {
		tb.Setenv(k, v)
	}

	for _, k := range inheritedRepoEnv {
		tb.Setenv(k, "") // registers restoration of the original value

		if err := os.Unsetenv(k); err != nil {
			tb.Fatal(err)
		}
	}

	dir, err := filepath.EvalSymlinks(tb.TempDir())
	if err != nil {
		tb.Fatal(err)
	}

	r := &Repo{t: tb, Dir: dir}
	r.Git("-c", "init.defaultBranch=main", "init", "--quiet")

	return r
}

// Git runs git with args in the repository and returns its stdout, failing
// the test if git fails.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		r.t.Fatalf("git %v: %v: %s", args, err, stderr.String())
	}

	return stdout.String()
}

// Write writes content to the repository-relative path, creating parent
// directories as needed.
func (r *Repo) Write(path, content string) {
	r.t.Helper()

	full := filepath.Join(r.Dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		r.t.Fatal(err)
	}

	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

// Commit stages every change in the working tree and commits it with
// message.
func (r *Repo) Commit(message string) {
	r.t.Helper()

	r.Git("add", "--all")
	r.Git("commit", "--quiet", "--allow-empty", "--message", message)
}
