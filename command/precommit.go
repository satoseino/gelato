package command

import (
	"context"
	"io"

	"github.com/satoseino/gelato/cfg"
	"github.com/satoseino/gelato/detect"
	"github.com/satoseino/gelato/git"
)

// PreCommit scans the lines added in the staged changes of the repository in
// the current directory.
type PreCommit struct {
	out reporter
	cfg cfg.Config
}

// Help returns the guide to reading and resolving a finding that kong prints
// under pre-commit --help.
func (*PreCommit) Help() string {
	return findingHelp(false)
}

// AfterApply loads the configuration for the repository in the current
// directory and sets up the report written to w.
func (c *PreCommit) AfterApply(g *Globals, w io.Writer) error {
	var err error

	c.out = newReporter(g, w)
	c.cfg, err = cfg.Load(g.Config, ".")

	return err
}

// Run scans the staged changes and reports what it finds. It returns
// ErrFindings when any issue was reported.
//
// git inherits the whole environment, so the temporary index that
// git commit -a gives its hooks through GIT_INDEX_FILE is the one scanned.
func (c *PreCommit) Run() error {
	ctx := context.Background()

	repo, err := git.Open(".")
	if err != nil {
		return err
	}

	engine, err := detect.New(c.cfg, detect.DefaultRules())
	if err != nil {
		return err
	}

	p := newPool(engine)

	var slots []*[]detect.Issue

	for fc, err := range repo.Staged(ctx) {
		if err != nil {
			p.wait()

			return err
		}

		slots = append(slots, p.scan(fc)...)
	}

	p.wait()

	var issues []detect.Issue
	for _, s := range slots {
		issues = append(issues, *s...)
	}

	for _, i := range issues {
		if err := c.out.issue(i, nil); err != nil {
			return err
		}
	}

	if err := c.out.summary(len(issues)); err != nil {
		return err
	}

	if len(issues) > 0 {
		return ErrFindings
	}

	return nil
}
