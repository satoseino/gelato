package git

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"iter"
	"strconv"
	"strings"
)

type (
	// Status is how a file changed.
	Status int

	// FileChange is one file's added lines from a patch.
	FileChange struct {
		Commit string
		Path   string
		Lines  []string
		Status Status
	}

	// patchParser reads one patch stream. It holds at most one line of
	// lookahead, pushed back with unread.
	patchParser struct {
		r       *bufio.Reader
		commit  string
		pending string
		hasLine bool
	}

	// fileHeader is what a file's extended headers say about it.
	fileHeader struct {
		gitLine   string // the "diff --git" line, without the prefix
		plusPath  string
		renameTo  string
		added     bool
		deleted   bool
		binary    bool
		submodule bool
	}
)

// The statuses a FileChange can have. A type change (e.g. file to symlink)
// appears in a patch as a deletion plus an addition, so it comes back as
// Added.
const (
	Modified Status = iota
	Added
	Renamed

	// submoduleMode is the mode git gives a gitlink entry.
	submoduleMode = "160000"
	devNull       = "/dev/null"
)

// String returns the status's name.
func (s Status) String() string {
	switch s {
	case Modified:
		return "Modified"
	case Added:
		return "Added"
	case Renamed:
		return "Renamed"
	}

	return "Status(" + strconv.Itoa(int(s)) + ")"
}

// ParsePatch parses the output of git diff or diff-tree run with -p
// --unified=0 and the pinned prefixes, and yields one FileChange per file
// that has content to scan. Deleted, binary and submodule entries are
// dropped, as are Modified changes with no added lines; Added and Renamed
// changes are kept even when they add no lines. A line holding only an
// object ID, as diff-tree --stdin prints before each commit's diff, sets
// Commit on the changes after it. The first malformed line ends the sequence
// with an error.
func ParsePatch(r io.Reader) iter.Seq2[FileChange, error] {
	return func(yield func(FileChange, error) bool) {
		p := &patchParser{r: bufio.NewReader(r)}

		for {
			fc, ok, err := p.next()
			if err != nil {
				yield(FileChange{}, err)

				return
			}

			if !ok || !yield(fc, nil) {
				return
			}
		}
	}
}

// next returns the next FileChange to yield, or false at the end of the
// stream.
func (p *patchParser) next() (FileChange, bool, error) {
	for {
		line, ok, err := p.read()
		if err != nil || !ok {
			return FileChange{}, false, err
		}

		switch {
		case isObjectID(line):
			p.commit = line
		case strings.HasPrefix(line, "diff --git "):
			fc, keep, err := p.file(strings.TrimPrefix(line, "diff --git "))
			if err != nil {
				return FileChange{}, false, err
			}

			if keep {
				return fc, true, nil
			}
		default:
			return FileChange{}, false, fmt.Errorf("unexpected patch line %q", line)
		}
	}
}

// file parses one file's extended headers and hunks, after its "diff --git"
// line, and reports whether the change should be yielded.
func (p *patchParser) file(gitLine string) (FileChange, bool, error) {
	h, err := p.header(gitLine)
	if err != nil {
		return FileChange{}, false, err
	}

	lines, err := p.hunks()
	if err != nil {
		return FileChange{}, false, err
	}

	if h.deleted || h.binary || h.submodule {
		return FileChange{}, false, nil
	}

	fc := FileChange{Commit: p.commit, Status: Modified, Lines: lines}

	switch {
	case h.added:
		fc.Status = Added
	case h.renameTo != "":
		fc.Status = Renamed
	}

	if fc.Status == Modified && len(lines) == 0 {
		return FileChange{}, false, nil
	}

	switch {
	case h.plusPath != "":
		fc.Path = h.plusPath
	case h.renameTo != "":
		fc.Path = h.renameTo
	default:
		if fc.Path, err = gitHeaderPath(h.gitLine); err != nil {
			return FileChange{}, false, err
		}
	}

	return fc, true, nil
}

// header reads extended header lines up to the first hunk or the next file.
// Header lines it doesn't need are skipped.
func (p *patchParser) header(gitLine string) (fileHeader, error) {
	h := fileHeader{gitLine: gitLine}

	for {
		line, ok, err := p.read()
		if err != nil || !ok {
			return h, err
		}

		if strings.HasPrefix(line, "@@ ") || strings.HasPrefix(line, "diff --git ") || isObjectID(line) {
			p.unread(line)

			return h, nil
		}

		key, value := headerField(line)
		switch key {
		case "new file mode":
			h.added = true
			h.submodule = h.submodule || value == submoduleMode
		case "deleted file mode":
			h.deleted = true
			h.submodule = h.submodule || value == submoduleMode
		case "old mode", "new mode":
			h.submodule = h.submodule || value == submoduleMode
		case "index":
			// "index <a>..<b>[ <mode>]"
			_, mode, _ := strings.Cut(value, " ")
			h.submodule = h.submodule || mode == submoduleMode
		case "rename to":
			if h.renameTo, err = decodePath(value); err != nil {
				return h, err
			}
		case "Binary files":
			h.binary = true
		case "+++":
			if value != devNull {
				path, err := decodePath(value)
				if err != nil {
					return h, err
				}

				if h.plusPath, ok = strings.CutPrefix(path, "b/"); !ok {
					return h, fmt.Errorf("patch path %q lacks the b/ prefix", path)
				}
			}
		}
	}
}

// hunks reads the hunks of one file and returns its added lines.
func (p *patchParser) hunks() ([]string, error) {
	var added []string

	for {
		line, ok, err := p.read()
		if err != nil || !ok {
			return added, err
		}

		switch {
		case strings.HasPrefix(line, `\`):
			// "\ No newline at end of file"
		case strings.HasPrefix(line, "@@ "):
			if added, err = p.hunk(line, added); err != nil {
				return nil, err
			}
		default:
			p.unread(line)

			return added, nil
		}
	}
}

// hunk reads exactly as many body lines as header announces and appends the
// added ones to added. Counting is what keeps an added "+++ x" or a removed
// "--- x" from being read as a file header.
func (p *patchParser) hunk(header string, added []string) ([]string, error) {
	oldCount, newCount, err := parseHunkHeader(header)
	if err != nil {
		return nil, err
	}

	for oldCount > 0 || newCount > 0 {
		line, ok, err := p.read()
		if err != nil {
			return nil, err
		}

		if !ok {
			return nil, fmt.Errorf("patch ends inside hunk %q", header)
		}

		switch {
		case strings.HasPrefix(line, "+") && newCount > 0:
			newCount--

			added = append(added, strings.ToValidUTF8(line[1:], "�"))
		case strings.HasPrefix(line, "-") && oldCount > 0:
			oldCount--
		case strings.HasPrefix(line, " ") && oldCount > 0 && newCount > 0:
			oldCount--
			newCount--
		case strings.HasPrefix(line, `\`):
		default:
			return nil, fmt.Errorf("unexpected line %q in hunk %q", line, header)
		}
	}

	return added, nil
}

// read returns the next line without its "\n", or false at the end of the
// stream. A final line without "\n" is still returned.
func (p *patchParser) read() (string, bool, error) {
	if p.hasLine {
		p.hasLine = false

		return p.pending, true, nil
	}

	line, err := p.r.ReadString('\n')
	if errors.Is(err, io.EOF) {
		return line, line != "", nil
	}

	if err != nil {
		return "", false, fmt.Errorf("reading patch: %s", err.Error())
	}

	return line[:len(line)-1], true, nil
}

func (p *patchParser) unread(line string) {
	p.pending, p.hasLine = line, true
}

// headerField splits an extended header line into its keyword and value.
func headerField(line string) (string, string) {
	for _, key := range []string{
		"new file mode", "deleted file mode", "old mode", "new mode",
		"index", "rename to", "Binary files", "+++",
	} {
		if value, ok := strings.CutPrefix(line, key+" "); ok {
			return key, value
		}
	}

	return "", line
}

// parseHunkHeader returns the old and new line counts of a
// "@@ -a[,b] +c[,d] @@[ <context>]" header.
func parseHunkHeader(header string) (int, int, error) {
	fields := strings.SplitN(header, " ", 5)
	if len(fields) < 4 || fields[3] != "@@" {
		return 0, 0, fmt.Errorf("malformed hunk header %q", header)
	}

	oldRange, okOld := strings.CutPrefix(fields[1], "-")
	newRange, okNew := strings.CutPrefix(fields[2], "+")

	oldCount, errOld := rangeCount(oldRange)
	newCount, errNew := rangeCount(newRange)

	if !okOld || !okNew || errOld != nil || errNew != nil {
		return 0, 0, fmt.Errorf("malformed hunk header %q", header)
	}

	return oldCount, newCount, nil
}

// rangeCount returns the line count of a hunk range "start[,count]", where a
// missing count means 1.
func rangeCount(r string) (int, error) {
	start, count, found := strings.Cut(r, ",")
	if _, err := strconv.ParseUint(start, 10, 64); err != nil {
		return 0, err
	}

	if !found {
		return 1, nil
	}

	n, err := strconv.ParseUint(count, 10, 31)

	return int(n), err
}

// decodePath decodes a path as git prints it in a patch: C-quoted when it
// holds special characters, otherwise raw. git appends a TAB to ---/+++
// paths containing a space, quoted or not; a path that itself ends in a TAB
// is always quoted, so the suffix is never part of the name.
func decodePath(s string) (string, error) {
	s = strings.TrimSuffix(s, "\t")
	if !strings.HasPrefix(s, `"`) {
		return s, nil
	}

	path, err := strconv.Unquote(s)
	if err != nil {
		return "", fmt.Errorf("malformed quoted path %s", s)
	}

	return path, nil
}

// gitHeaderPath returns the path of a "diff --git" header whose two paths
// are identical, as they are for new empty files and mode-only changes.
func gitHeaderPath(header string) (string, error) {
	if strings.HasPrefix(header, `"`) {
		quoted, err := strconv.QuotedPrefix(header)
		if err != nil {
			return "", fmt.Errorf("malformed diff header %q", header)
		}

		a, errA := decodePath(quoted)
		b, errB := decodePath(strings.TrimPrefix(header[len(quoted):], " "))
		path, okA := strings.CutPrefix(a, "a/")
		bPath, okB := strings.CutPrefix(b, "b/")

		if errA != nil || errB != nil || !okA || !okB || path != bPath {
			return "", fmt.Errorf("malformed diff header %q", header)
		}

		return path, nil
	}

	// "a/<p> b/<p>": the separator sits at the midpoint.
	n := (len(header) - len("a/ b/")) / 2
	if n <= 0 || len(header) != 2*n+len("a/ b/") ||
		header[:2] != "a/" || header[2+n:2+n+3] != " b/" || header[2:2+n] != header[2+n+3:] {
		return "", fmt.Errorf("malformed diff header %q", header)
	}

	return header[2 : 2+n], nil
}

// isObjectID reports whether line is a 40-digit (SHA-1) or 64-digit
// (SHA-256) lowercase hex object ID.
func isObjectID(line string) bool {
	if len(line) != 40 && len(line) != 64 {
		return false
	}

	for _, c := range line {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}

	return true
}
