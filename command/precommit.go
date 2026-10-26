package command

import (
	"errors"

	"github.com/satoseino/gelato/cfg"
)

// PreCommit scans the lines added in the staged changes of the repository in
// the current directory.
type PreCommit struct {
	cfg cfg.Config
}

// AfterApply loads the configuration for the repository in the current
// directory.
func (c *PreCommit) AfterApply(g *Globals) error {
	var err error

	c.cfg, err = cfg.Load(g.Config, ".")

	return err
}

// Run scans the staged changes.
func (c *PreCommit) Run() error {
	return errors.New("pre-commit: not implemented")
}
