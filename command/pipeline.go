package command

import (
	"runtime"
	"sync"

	"github.com/satoseino/gelato/detect"
	"github.com/satoseino/gelato/git"
)

type (
	// pool scans chunks on runtime.GOMAXPROCS(0) goroutines shared by every
	// producer of FileChanges.
	pool struct {
		engine *detect.Engine
		jobs   chan job
		wg     sync.WaitGroup
	}

	// job is a chunk to scan and the slot its issues are stored in.
	job struct {
		issues *[]detect.Issue
		chunk  detect.Chunk
	}
)

// newPool starts a pool scanning with e. Call wait to stop it.
func newPool(e *detect.Engine) *pool {
	p := &pool{engine: e, jobs: make(chan job)}

	for range runtime.GOMAXPROCS(0) {
		p.wg.Go(func() {
			for j := range p.jobs {
				*j.issues = p.engine.Scan(j.chunk)
			}
		})
	}

	return p
}

// scan queues fc's chunks and returns one slot per chunk, in chunk order.
// A slot holds its chunk's issues once wait has returned. It is safe to call
// from several goroutines.
func (p *pool) scan(fc git.FileChange) []*[]detect.Issue {
	chunks := chunksOf(fc)
	slots := make([]*[]detect.Issue, len(chunks))

	for k, c := range chunks {
		slots[k] = new([]detect.Issue)
		p.jobs <- job{chunk: c, issues: slots[k]}
	}

	return slots
}

// wait stops accepting chunks and returns once every queued chunk is
// scanned.
func (p *pool) wait() {
	close(p.jobs)
	p.wg.Wait()
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
