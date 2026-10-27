package git

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"strings"
)

type (
	// Commit is a commit to scan and the parent it is diffed against.
	Commit struct {
		// ID is the commit's object ID.
		ID string
		// FirstParent is the object ID of the commit's first parent, or "" for
		// a root commit or a shallow-clone boundary, which are diffed against
		// the empty tree.
		FirstParent string
	}

	// CommitInfo is the metadata a report shows for a commit.
	CommitInfo struct {
		// Hash is the commit's full object ID.
		Hash string
		// Time is the committer date in strict ISO 8601 format.
		Time string
		// Subject is the first line of the commit message.
		Subject string
	}
)

// Commits returns every commit reachable from a local branch, a
// remote-tracking branch or HEAD, each once, children before their parents.
// A repository with no commits has none.
func (r *Repo) Commits(ctx context.Context) ([]Commit, error) {
	args := []string{"--topo-order", "--parents", "--branches", "--remotes"}

	// HEAD covers a detached checkout. rev-parse fails when HEAD is unborn,
	// and rev-list would then fail on it.
	var re *runError

	_, err := r.output(ctx, "rev-parse", "--verify", "-q", "HEAD")

	switch {
	case err == nil:
		args = append(args, "HEAD")
	case !errors.As(err, &re):
		return nil, err
	}

	out, err := r.output(ctx, "rev-list", append(args, "--")...)
	if err != nil {
		return nil, err
	}

	var commits []Commit

	for line := range strings.Lines(out) {
		fields := strings.Fields(line)
		if len(fields) == 0 || !isObjectID(fields[0]) {
			return nil, fmt.Errorf("unexpected git rev-list line %q", strings.TrimSuffix(line, "\n"))
		}

		c := Commit{ID: fields[0]}

		// A shallow boundary's parents are absent, as a root's are.
		if len(fields) > 1 {
			c.FirstParent = fields[1]
		}

		commits = append(commits, c)
	}

	return commits, nil
}

// Patches returns the changes each commit made relative to its first parent,
// or to the empty tree when it has none, in the order of commits. Every
// FileChange has Commit set. Breaking out of the loop stops the git process.
func (r *Repo) Patches(ctx context.Context, commits []Commit) iter.Seq2[FileChange, error] {
	var stdin strings.Builder

	for _, c := range commits {
		// Naming the parent is what makes diff-tree print a merge's diff.
		if c.FirstParent != "" {
			stdin.WriteString(c.ID + " " + c.FirstParent + "\n")
		} else {
			stdin.WriteString(c.ID + "\n")
		}
	}

	return r.patches(ctx, strings.NewReader(stdin.String()), "diff-tree", "--stdin", "--root")
}

// CommitInfo returns the metadata of the commits ids, keyed by ID.
func (r *Repo) CommitInfo(ctx context.Context, ids []string) (map[string]CommitInfo, error) {
	infos := make(map[string]CommitInfo, len(ids))
	if len(ids) == 0 {
		return infos, nil
	}

	// --no-show-signature and --encoding pin log.showSignature and
	// i18n.logOutputEncoding.
	var out string

	err := r.run(ctx, strings.NewReader(strings.Join(ids, "\n")+"\n"), "log",
		[]string{"--no-walk=unsorted", "--stdin", "--no-show-signature", "--encoding=UTF-8", "--format=%H%x00%cI%x00%s"},
		func(stdout io.Reader) error {
			b, err := io.ReadAll(stdout)
			out = string(b)

			return err
		})
	if err != nil {
		return nil, err
	}

	for line := range strings.Lines(out) {
		fields := strings.Split(strings.TrimSuffix(line, "\n"), "\x00")
		if len(fields) != 3 || !isObjectID(fields[0]) {
			return nil, fmt.Errorf("unexpected git log line %q", strings.TrimSuffix(line, "\n"))
		}

		infos[fields[0]] = CommitInfo{Hash: fields[0], Time: fields[1], Subject: strings.ToValidUTF8(fields[2], "�")}
	}

	for _, id := range ids {
		if _, ok := infos[id]; !ok {
			return nil, fmt.Errorf("git log printed no metadata for commit %s", id)
		}
	}

	return infos, nil
}
