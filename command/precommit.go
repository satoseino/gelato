package command

import (
	"context"
	"io"
	"runtime"
	"sync"

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

	var chunks []detect.Chunk

	for fc, err := range repo.Staged(ctx) {
		if err != nil {
			return err
		}

		chunks = append(chunks, chunksOf(fc)...)
	}

	issues := scanChunks(engine, chunks)
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

// chunksOf returns the chunks to scan for fc: its added lines, and for a new
// path the path itself, so that secrets in file names are caught. The path
// is a chunk of its own so that its issues show only the path.
func chunksOf(fc git.FileChange) []detect.Chunk {
	var chunks []detect.Chunk

	if len(fc.Lines) > 0 {
		chunks = append(chunks, detect.Chunk{Path: fc.Path, Lines: fc.Lines})
	}

	if fc.Status == git.Added || fc.Status == git.Renamed {
		chunks = append(chunks, detect.Chunk{Path: fc.Path, Lines: []string{fc.Path}})
	}

	return chunks
}

// scanChunks scans chunks on runtime.GOMAXPROCS(0) goroutines and returns
// their issues in chunk order, so output doesn't depend on scheduling.
func scanChunks(e *detect.Engine, chunks []detect.Chunk) []detect.Issue {
	results := make([][]detect.Issue, len(chunks))
	seqs := make(chan int)

	var wg sync.WaitGroup

	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for seq := range seqs {
				results[seq] = e.Scan(chunks[seq])
			}
		})
	}

	for seq := range chunks {
		seqs <- seq
	}

	close(seqs)
	wg.Wait()

	var issues []detect.Issue
	for _, r := range results {
		issues = append(issues, r...)
	}

	return issues
}
