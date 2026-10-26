package detect

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/satoseino/gelato/cfg"
)

type (
	// Engine scans chunks against a configuration and a rule set. It is
	// immutable and safe for concurrent use by multiple goroutines.
	Engine struct {
		rules []Rule
		cfg   cfg.Config
	}

	// issueKey identifies an issue for within-chunk deduplication.
	issueKey struct {
		typ    IssueType
		detail string
		match  string
	}

	// collector accumulates the issues of one Scan call, dropping repeats.
	collector struct {
		seen   map[issueKey]struct{}
		chunk  Chunk
		issues []Issue
	}
)

const (
	// Entropy thresholds at tartufo's fixed sensitivity of 75%, scaled by the
	// bits each character of the encoding carries. A candidate is flagged
	// only when its entropy is strictly greater.
	base64EntropyLimit = 0.75 * 6
	hexEntropyLimit    = 0.75 * 4

	// minCandidateLen is the minimum length, in runes, of an entropy
	// candidate.
	minCandidateLen = 20
)

// New returns an Engine for c and rules. Rules run in Name order, so output
// doesn't depend on the order they are given in. A rule whose Pattern has
// more than one capture group is an error, because Python's findall would
// return tuples for it.
func New(c cfg.Config, rules []Rule) (*Engine, error) {
	for _, r := range rules {
		if r.Pattern == nil {
			return nil, fmt.Errorf("rule %q: no pattern", r.Name)
		}

		if n := r.Pattern.NumSubexp(); n > 1 {
			return nil, fmt.Errorf("rule %q: pattern has %d capture groups, at most 1 is allowed", r.Name, n)
		}
	}

	sorted := slices.Clone(rules)
	slices.SortStableFunc(sorted, func(a, b Rule) int { return cmp.Compare(a.Name, b.Name) })

	return &Engine{rules: sorted, cfg: c}, nil
}

// Scan returns the issues found in c: regex issues by rule name then
// position, then entropy issues by line then position. An issue with the same
// type, detail and match as an earlier one in c is reported once. A chunk
// whose path matches an `exclude-path-patterns` entry yields nothing.
func (e *Engine) Scan(c Chunk) []Issue {
	for _, p := range e.cfg.ExcludePaths {
		if p.MatchString(c.Path) {
			return nil
		}
	}

	col := &collector{chunk: c, seen: map[issueKey]struct{}{}}

	if e.cfg.Regex {
		e.scanRegex(col)
	}

	if e.cfg.Entropy {
		e.scanEntropy(col)
	}

	return col.issues
}

// scanRegex runs every rule over the chunk's lines joined with "\n", like
// tartufo's findall over a whole chunk, so a pattern can span lines.
func (e *Engine) scanRegex(col *collector) {
	path := col.chunk.Path
	text := strings.Join(col.chunk.Lines, "\n")

	for _, r := range e.rules {
		if r.PathPattern != nil && !matchesAtStart(r.PathPattern, path) {
			continue
		}

		group := r.Pattern.NumSubexp() // 0 or 1, checked by New
		for _, m := range r.Pattern.FindAllStringSubmatch(text, -1) {
			match := m[group]

			sig := Signature(match, path)
			if signatureExcluded(e.cfg.ExcludeSignatures, match, sig) {
				continue
			}

			col.add(RegexIssue, r.Name, match, sig)
		}
	}
}

// scanEntropy checks every base64 and hex candidate of every word of every
// non-empty line, base64 candidates of a word before its hex candidates.
func (e *Engine) scanEntropy(col *collector) {
	for _, line := range col.chunk.Lines {
		for _, word := range strings.FieldsFunc(line, isPythonSpace) {
			for _, s := range candidates(word, isBase64Rune) {
				e.evaluateEntropy(col, line, s, base64EntropyLimit)
			}

			for _, s := range candidates(word, isHexRune) {
				e.evaluateEntropy(col, line, s, hexEntropyLimit)
			}
		}
	}
}

// evaluateEntropy adds an issue for s, found in line, if its entropy exceeds
// limit and neither its signature nor an `exclude-entropy-patterns` entry
// suppresses it.
func (e *Engine) evaluateEntropy(col *collector, line, s string, limit float64) {
	path := col.chunk.Path

	sig := Signature(s, path)
	if signatureExcluded(e.cfg.ExcludeSignatures, s, sig) {
		return
	}

	if entropy(s) <= limit {
		return
	}

	for _, x := range e.cfg.ExcludeEntropyPatterns {
		if x.Pattern.MatchString(line) && (x.PathPattern == nil || x.PathPattern.MatchString(path)) {
			return
		}
	}

	col.add(EntropyIssue, "", s, sig)
}

// add records an issue unless an identical one was already recorded.
func (col *collector) add(typ IssueType, detail, match, sig string) {
	key := issueKey{typ: typ, detail: detail, match: match}
	if _, ok := col.seen[key]; ok {
		return
	}

	col.seen[key] = struct{}{}
	col.issues = append(col.issues, Issue{
		Type:      typ,
		Detail:    detail,
		Match:     match,
		Path:      col.chunk.Path,
		Signature: sig,
		Chunk:     col.chunk,
	})
}

// matchesAtStart reports whether re matches s at its start, like Python's
// re.match. The leftmost match starts at 0 whenever any match does.
func matchesAtStart(re *regexp.Regexp, s string) bool {
	loc := re.FindStringIndex(s)

	return loc != nil && loc[0] == 0
}

// candidates returns the maximal runs of runes satisfying in that are at
// least minCandidateLen runes long. Like Python's len, the length counts
// runes (code points), not bytes.
func candidates(word string, in func(rune) bool) []string {
	var out []string

	start, n := -1, 0

	flush := func(end int) {
		if start >= 0 && n >= minCandidateLen {
			out = append(out, word[start:end])
		}

		start, n = -1, 0
	}

	for i, r := range word {
		if !in(r) {
			flush(i)

			continue
		}

		if start < 0 {
			start = i
		}

		n++
	}

	flush(len(word))

	return out
}

// entropy returns the Shannon entropy of s over its runes, in bits. Terms
// are summed in order of each rune's first occurrence, as Python's Counter
// iterates, and each term is rounded before it is added (no fused
// multiply-add), so the arithmetic follows tartufo's. math.Log2 can still
// differ from the C library's log2 in the last bit, so a result may differ
// from tartufo's by an ulp; entropies exactly at a limit are sums of
// power-of-two terms, which both compute exactly.
func entropy(s string) float64 {
	if s == "" {
		return 0
	}

	counts := map[rune]int{}

	var order []rune

	for _, r := range s {
		if counts[r] == 0 {
			order = append(order, r)
		}

		counts[r]++
	}

	size := float64(utf8.RuneCountInString(s))

	var h float64

	for _, r := range order {
		p := float64(counts[r]) / size
		h += float64(-p * math.Log2(p)) // conversion prevents FMA fusion
	}

	return h
}

// isPythonSpace reports whether Python's str.split treats r as whitespace.
// That is unicode.IsSpace plus U+001C–U+001F, which Python counts as
// whitespace and Go doesn't.
func isPythonSpace(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1C && r <= 0x1F)
}

// isBase64Rune reports whether r is in tartufo's case-insensitive base64
// class [A-Z0-9=+/_-]. Python's case folding also maps U+0130 (İ), U+0131
// (ı), U+017F (ſ) and U+212A (Kelvin sign) into A-Z.
func isBase64Rune(r rune) bool {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		return true
	}

	switch r {
	case '=', '+', '/', '_', '-', 0x0130, 0x0131, 0x017F, 0x212A:
		return true
	}

	return false
}

// isHexRune reports whether r is in tartufo's case-insensitive hex class
// [0-9A-F]. No non-ASCII rune folds into it.
func isHexRune(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'A' && r <= 'F') || (r >= 'a' && r <= 'f')
}
