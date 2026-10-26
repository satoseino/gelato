// Package git runs the git CLI with pinned options and parses its patches.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
)

type (
	// Repo is a git working tree that commands run against.
	Repo struct {
		gitPath string
		root    string
		env     []string
	}

	// Option configures Open.
	Option func(*options)

	options struct {
		withoutRepoEnv bool
	}

	// runError is a git process that exited unsuccessfully.
	runError struct {
		err    error
		sub    string
		stderr string
	}
)

var (
	// minVersion is the oldest git release gelato supports.
	minVersion = [3]int{2, 30, 0}

	// configArgs pin the config keys that could change paths or run programs
	// in any git command.
	configArgs = []string{
		"-c", "core.quotePath=false",
		"-c", "diff.noprefix=false",
		"-c", "core.fsmonitor=false",
	}

	// patchArgs pin the patch format ParsePatch expects against config such as
	// color.ui, diff.mnemonicPrefix, diff.relative, diff.submodule,
	// diff.algorithm, diff.interHunkContext, diff.external and textconv
	// drivers. Without --inter-hunk-context=0, hunks would gain context lines,
	// which diff.suppressBlankEmpty prints as empty lines. -M50% and Myers are
	// libgit2's defaults, which tartufo used.
	patchArgs = []string{
		"-p", "--no-color", "--no-ext-diff", "--no-textconv", "--no-relative",
		"--src-prefix=a/", "--dst-prefix=b/", "-M50%", "--unified=0",
		"--inter-hunk-context=0", "--diff-algorithm=myers", "--submodule=short",
	}

	// repoEnv lists the variables that route git to a repository, index or
	// config other than the one in the working directory, as printed by
	// git rev-parse --local-env-vars.
	repoEnv = []string{
		"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_PREFIX",
		"GIT_SHALLOW_FILE", "GIT_GRAFT_FILE", "GIT_NO_REPLACE_OBJECTS",
		"GIT_REPLACE_REF_BASE", "GIT_IMPLICIT_WORK_TREE", "GIT_CONFIG",
		"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	}

	// errStopped ends a git process early when the consumer stops iterating.
	errStopped = errors.New("iteration stopped")
)

// WithoutRepoEnv makes git ignore the repository-routing environment
// variables (GIT_DIR, GIT_INDEX_FILE, ...), so the repository is the one Open
// was given even when gelato runs inside another repository's hook. By
// default the whole environment is inherited, so that a pre-commit hook's
// GIT_INDEX_FILE reaches git diff --cached.
func WithoutRepoEnv() Option {
	return func(o *options) { o.withoutRepoEnv = true }
}

// Open finds the git executable, checks that it is at least version 2.30,
// and returns the working tree that contains dir.
func Open(dir string, opts ...Option) (*Repo, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	gitPath, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("git executable not found in PATH")
	}

	env := os.Environ()
	if o.withoutRepoEnv {
		env = slices.DeleteFunc(env, func(kv string) bool {
			key, _, _ := strings.Cut(kv, "=")

			return slices.Contains(repoEnv, key)
		})
	}

	// Never take index.lock, which git commit holds while hooks run.
	env = append(env, "GIT_OPTIONAL_LOCKS=0")

	// git version runs in the current directory, so a dir that doesn't exist
	// is reported by rev-parse as not a repository rather than as a failed
	// version check.
	r := &Repo{gitPath: gitPath, env: env}
	ctx := context.Background()

	version, err := r.output(ctx, "version")
	if err != nil {
		return nil, err
	}

	if versionErr := checkVersion(version); versionErr != nil {
		return nil, versionErr
	}

	r.root = dir

	root, err := r.output(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		var re *runError
		if errors.As(err, &re) {
			return nil, fmt.Errorf("%s is not a valid git repository: %s", dir, re.stderr)
		}

		return nil, fmt.Errorf("%s is not a valid git repository: %s", dir, err.Error())
	}

	r.root = strings.TrimSuffix(root, "\n")

	return r, nil
}

// Root returns the absolute path of the working tree's top-level directory.
func (r *Repo) Root() string {
	return r.root
}

// Staged returns the changes staged in the index relative to HEAD, or
// relative to the empty tree on an unborn branch, where every staged file is
// Added.
func (r *Repo) Staged(ctx context.Context) iter.Seq2[FileChange, error] {
	return r.patches(ctx, nil, "diff", "--cached")
}

// patches runs a patch-producing subcommand with patchArgs and yields its
// parsed changes. Breaking out of the loop stops the git process.
func (r *Repo) patches(ctx context.Context, stdin io.Reader, sub string, args ...string) iter.Seq2[FileChange, error] {
	return func(yield func(FileChange, error) bool) {
		err := r.run(ctx, stdin, sub, append(slices.Clone(patchArgs), args...), func(stdout io.Reader) error {
			for fc, err := range ParsePatch(stdout) {
				if err != nil {
					return err
				}

				if !yield(fc, nil) {
					return errStopped
				}
			}

			return nil
		})
		if err != nil && !errors.Is(err, errStopped) {
			yield(FileChange{}, err)
		}
	}
}

// output runs a subcommand and returns its whole stdout.
func (r *Repo) output(ctx context.Context, sub string, args ...string) (string, error) {
	var out string

	err := r.run(ctx, nil, sub, args, func(stdout io.Reader) error {
		b, err := io.ReadAll(stdout)
		out = string(b)

		return err
	})

	return out, err
}

// run starts "git <configArgs> <sub> <args>" in the working tree, feeds it
// stdin if non-nil, and passes its stdout to consume as a stream. When
// consume returns errStopped the process is killed and errStopped returned.
// A non-zero exit is reported with git's stderr.
func (r *Repo) run(ctx context.Context, stdin io.Reader, sub string, args []string, consume func(io.Reader) error) error {
	procCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	cmd := exec.CommandContext(procCtx, r.gitPath, slices.Concat(configArgs, []string{sub}, args)...)
	cmd.Dir = r.root
	cmd.Env = r.env
	cmd.Stdin = stdin

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git %s: %s", sub, err.Error())
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git %s: %s", sub, err.Error())
	}

	consumeErr := consume(stdout)
	if errors.Is(consumeErr, errStopped) {
		cancel()
	} else if consumeErr != nil {
		// Drain so git can exit, then prefer its own error: a failing git
		// often leaves a truncated patch behind.
		_, _ = io.Copy(io.Discard, stdout)
	}

	waitErr := cmd.Wait()

	switch {
	case errors.Is(consumeErr, errStopped):
		return errStopped
	case ctx.Err() != nil:
		return ctx.Err()
	case waitErr != nil:
		return &runError{sub: sub, err: waitErr, stderr: strings.TrimSpace(stderr.String())}
	case consumeErr != nil:
		return fmt.Errorf("git %s: %s", sub, consumeErr.Error())
	}

	return nil
}

// Error implements error.
func (e *runError) Error() string {
	return fmt.Sprintf("git %s failed: %s: %s", e.sub, e.err.Error(), e.stderr)
}

// checkVersion parses "git version" output and fails below minVersion.
func checkVersion(out string) error {
	v, err := parseVersion(out)
	if err != nil {
		return err
	}

	if slices.Compare(v[:], minVersion[:]) < 0 {
		return fmt.Errorf("git %d.%d.%d is too old: gelato requires git %d.%d.%d or newer",
			v[0], v[1], v[2], minVersion[0], minVersion[1], minVersion[2])
	}

	return nil
}

// parseVersion returns the first three numeric components of "git version"
// output, such as "git version 2.39.3 (Apple Git-146)" or
// "git version 2.45.1.windows.1". A missing patch component is 0.
func parseVersion(out string) ([3]int, error) {
	var v [3]int

	fields := strings.Fields(out)
	if len(fields) < 3 || fields[0] != "git" || fields[1] != "version" {
		return v, fmt.Errorf("unrecognized git version output %q", strings.TrimSpace(out))
	}

	parts := strings.Split(fields[2], ".")
	for i := range v {
		if i >= len(parts) {
			break
		}

		n, err := strconv.Atoi(parts[i])
		if err != nil {
			if i < 2 {
				return v, fmt.Errorf("unrecognized git version output %q", strings.TrimSpace(out))
			}

			break
		}

		v[i] = n
	}

	if len(parts) < 2 {
		return v, fmt.Errorf("unrecognized git version output %q", strings.TrimSpace(out))
	}

	return v, nil
}
