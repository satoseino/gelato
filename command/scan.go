package command

import (
	"errors"

	"github.com/satoseino/gelato/cfg"
)

// Scan scans the lines added across a repository's history.
type Scan struct {
	RepoPath string `arg:"" help:"Repository to scan. Defaults to repo-path from --config, else the current directory." name:"repo-path" optional:""`
	cfg      cfg.Config
}

// AfterApply loads the configuration for the repository being scanned.
func (c *Scan) AfterApply(g *Globals) error {
	var err error

	c.cfg, err = cfg.Load(g.Config, c.RepoPath)

	return err
}

// Run scans the repository's history.
func (c *Scan) Run() error {
	return errors.New("scan: not implemented")
}
