package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"

	"github.com/satoseino/gelato/cfg"
	"github.com/satoseino/gelato/detect"
	"github.com/satoseino/gelato/git"
)

type (
	// Scan scans the lines added across a repository's history.
	Scan struct {
		out      reporter
		RepoPath string `arg:"" help:"Repository to scan. Defaults to repo-path from --config, else the current directory." name:"repo-path" optional:""`
		cfg      cfg.Config
	}

	// commitSlot is the issues of one chunk, tagged with the rev-list index
	// of the commit it was found in.
	commitSlot struct {
		issues *[]detect.Issue
		commit int
	}

	// finding is an issue and the rev-list index of its commit.
	finding struct {
		issue  detect.Issue
		commit int
	}

	// issueKey identifies the same finding across commits.
	issueKey struct {
		signature string
		typ       detect.IssueType
		detail    string
	}
)

// Help returns the guide to reading and resolving a finding that kong prints
// under scan --help.
func (*Scan) Help() string {
	return findingHelp(true)
}

// AfterApply loads the configuration for the repository being scanned and
// sets up the report written to w.
func (c *Scan) AfterApply(g *Globals, w io.Writer) error {
	var err error

	c.out = newReporter(g, w)
	c.cfg, err = cfg.Load(g.Config, c.RepoPath)

	return err
}

// Run scans every commit reachable from a branch, a remote-tracking branch
// or HEAD, and reports each finding once, at the oldest commit that adds it.
// It returns ErrFindings when any issue was reported.
//
// Repository-routing variables such as GIT_DIR are ignored, so the
// repository scanned is always the one RepoPath resolved to.
func (c *Scan) Run() error {
	ctx := context.Background()

	repo, err := git.Open(c.cfg.RepoPath, git.WithoutRepoEnv())
	if err != nil {
		return err
	}

	engine, err := detect.New(c.cfg, detect.DefaultRules())
	if err != nil {
		return err
	}

	commits, err := repo.Commits(ctx)
	if err != nil {
		return err
	}

	found, err := scanHistory(ctx, repo, engine, commits)
	if err != nil {
		return err
	}

	found = dedup(found)

	meta, err := commitMeta(ctx, repo, commits, found)
	if err != nil {
		return err
	}

	for _, f := range found {
		info := meta[f.commit]
		if err := c.out.issue(f.issue, &info); err != nil {
			return err
		}
	}

	if err := c.out.summary(len(found)); err != nil {
		return err
	}

	if len(found) > 0 {
		return ErrFindings
	}

	return nil
}

// scanHistory splits commits into one contiguous batch per CPU, streams each
// batch's patches from its own git process into a shared pool, and returns
// the issues in rev-list order, then chunk and issue order.
func scanHistory(ctx context.Context, repo *git.Repo, e *detect.Engine, commits []git.Commit) ([]finding, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	index := make(map[string]int, len(commits))
	for k, c := range commits {
		index[c.ID] = k
	}

	n := min(runtime.GOMAXPROCS(0), len(commits))
	slots := make([][]commitSlot, n)
	errs := make([]error, n)
	p := newPool(e)

	var wg sync.WaitGroup

	for b := range n {
		batch := commits[b*len(commits)/n : (b+1)*len(commits)/n]

		wg.Go(func() {
			for fc, err := range repo.Patches(ctx, batch) {
				// Defaulting to index 0 would attribute the change to the
				// newest commit and dedup it at the wrong place.
				k, ok := index[fc.Commit]
				if err == nil && !ok {
					err = fmt.Errorf("git diff-tree printed %s for unknown commit %q", fc.Path, fc.Commit)
				}

				if err != nil {
					errs[b] = err

					cancel()

					return
				}

				for _, s := range p.scan(fc) {
					slots[b] = append(slots[b], commitSlot{commit: k, issues: s})
				}
			}
		})
	}

	wg.Wait()
	p.wait()

	if err := firstError(errs); err != nil {
		return nil, err
	}

	var all []finding

	for _, batch := range slots {
		for _, s := range batch {
			for _, i := range *s.issues {
				all = append(all, finding{issue: i, commit: s.commit})
			}
		}
	}

	return all, nil
}

// firstError returns the first error that is not a cancellation caused by
// another batch's failure.
func firstError(errs []error) error {
	var canceled error

	for _, err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, context.Canceled):
			canceled = err
		default:
			return err
		}
	}

	return canceled
}

// dedup keeps one issue per (signature, type, detail): the one in the oldest
// commit, which is the highest rev-list index, and within that commit the
// first. A merge re-shows its side branch's lines, and a secret that stays in
// a renamed or rewritten file can be added again; one exclude-signatures
// entry covers every repeat, so repeats only add noise. found keeps its
// order.
func dedup(found []finding) []finding {
	keep := map[issueKey]int{}

	for k, f := range found {
		key := issueKey{signature: f.issue.Signature, typ: f.issue.Type, detail: f.issue.Detail}
		if prev, ok := keep[key]; !ok || found[prev].commit < f.commit {
			keep[key] = k
		}
	}

	kept := found[:0]

	for k, f := range found {
		key := issueKey{signature: f.issue.Signature, typ: f.issue.Type, detail: f.issue.Detail}
		if keep[key] == k {
			kept = append(kept, f)
		}
	}

	return kept
}

// commitMeta returns the metadata of every commit with an issue in found,
// keyed by rev-list index.
func commitMeta(ctx context.Context, repo *git.Repo, commits []git.Commit, found []finding) (map[int]git.CommitInfo, error) {
	var ids []string

	meta := map[int]git.CommitInfo{}

	for _, f := range found {
		if _, ok := meta[f.commit]; !ok {
			meta[f.commit] = git.CommitInfo{}
			ids = append(ids, commits[f.commit].ID)
		}
	}

	infos, err := repo.CommitInfo(ctx, ids)
	if err != nil {
		return nil, err
	}

	for k := range meta {
		meta[k] = infos[commits[k].ID]
	}

	return meta, nil
}
