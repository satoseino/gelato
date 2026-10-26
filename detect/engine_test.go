package detect

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/satoseino/gelato/cfg"
)

// found is a comparable view of an Issue.
type found struct {
	Type   IssueType
	Detail string
	Match  string
}

// High-entropy fixtures are assembled at runtime so that no token-shaped
// literal is ever committed.
var (
	// highB64 is 30 distinct base64 runes, entropy log2(30) > 4.5, with no hex
	// run long enough to be a candidate.
	highB64 = runeRange('A', 'Z') + runeRange('a', 'd')
	// highHex is a 20-rune hex string with entropy above 3.0 and below 4.5.
	highHex = (runeRange('0', '9') + runeRange('a', 'f') + runeRange('0', '9'))[:20]
	// limitB64 has entropy exactly 4.5: 16 runes seen once and 8 seen twice,
	// none of them hex.
	limitB64 = runeRange('G', 'V') + runeRange('g', 'n') + runeRange('g', 'n')
	// limitHex has entropy exactly 3.0: 8 hex runes seen 3 times each.
	limitHex = strings.Repeat(runeRange('0', '7'), 3)
)

func runeRange(from, to rune) string {
	var b strings.Builder
	for r := from; r <= to; r++ {
		b.WriteRune(r)
	}

	return b.String()
}

func summarize(issues []Issue) []found {
	out := make([]found, 0, len(issues))
	for _, i := range issues {
		out = append(out, found{Type: i.Type, Detail: i.Detail, Match: i.Match})
	}

	return out
}

func entropyOnly() cfg.Config {
	return cfg.Config{Entropy: true}
}

func TestEntropyGoldens(t *testing.T) {
	data, err := os.ReadFile("testdata/entropy.json")
	if err != nil {
		t.Fatal(err)
	}

	var goldens []struct {
		Input   string  `json:"input"`
		Entropy float64 `json:"entropy"`
	}

	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}

	if len(goldens) == 0 {
		t.Fatal("no goldens")
	}

	// math.Log2 can differ from C's log2 in the last bit; see entropy.
	const ulps = 4

	for _, g := range goldens {
		got := entropy(g.Input)
		if diff := math.Abs(got - g.Entropy); diff > ulps*ulp(g.Entropy) {
			t.Errorf("entropy(%q) = %v, want %v", g.Input, got, g.Entropy)
		}
	}
}

// ulp returns the distance from x to the next larger float64.
func ulp(x float64) float64 {
	return math.Nextafter(x, math.Inf(1)) - x
}

func TestEntropyFixtures(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		limit float64
		above bool
	}{
		{name: "high base64", s: highB64, limit: base64EntropyLimit, above: true},
		{name: "high hex", s: highHex, limit: hexEntropyLimit, above: true},
		{name: "high hex is not high base64", s: highHex, limit: base64EntropyLimit, above: false},
		{name: "base64 at limit", s: limitB64, limit: base64EntropyLimit, above: false},
		{name: "hex at limit", s: limitHex, limit: hexEntropyLimit, above: false},
	}

	for _, tt := range tests {
		if got := entropy(tt.s) > tt.limit; got != tt.above {
			t.Errorf("%s: entropy(%q) = %v, above %v = %v, want %v",
				tt.name, tt.s, entropy(tt.s), tt.limit, got, tt.above)
		}
	}

	if got := entropy(limitB64); got != base64EntropyLimit {
		t.Errorf("entropy(limitB64) = %v, want exactly %v", got, base64EntropyLimit)
	}

	if got := entropy(limitHex); got != hexEntropyLimit {
		t.Errorf("entropy(limitHex) = %v, want exactly %v", got, hexEntropyLimit)
	}
}

func TestCandidates(t *testing.T) {
	folded := "İıſK" // İ ı ſ K, which Python folds into A-Z

	tests := []struct {
		name      string
		word      string
		base64Out []string
		hexOut    []string
	}{
		{name: "19 runes", word: strings.Repeat("x", 19)},
		{name: "20 runes", word: strings.Repeat("x", 20), base64Out: []string{strings.Repeat("x", 20)}},
		{
			name:      "base64 and hex runs",
			word:      "zz" + strings.Repeat("ab12", 5) + "zz",
			base64Out: []string{"zz" + strings.Repeat("ab12", 5) + "zz"},
			hexOut:    []string{strings.Repeat("ab12", 5)},
		},
		{
			name:      "punctuation splits runs",
			word:      strings.Repeat("x", 20) + "." + strings.Repeat("y", 6) + ":" + strings.Repeat("z", 21),
			base64Out: []string{strings.Repeat("x", 20), strings.Repeat("z", 21)},
		},
		{
			name:      "base64 punctuation stays in the run",
			word:      strings.Repeat("x", 5) + "=+/_-" + strings.Repeat("y", 10),
			base64Out: []string{strings.Repeat("x", 5) + "=+/_-" + strings.Repeat("y", 10)},
		},
		{
			name:      "Python case folds stay in the run",
			word:      strings.Repeat("ab"+folded, 4),
			base64Out: []string{strings.Repeat("ab"+folded, 4)},
		},
		{
			name: "length counts runes, not bytes",
			word: strings.Repeat(folded, 4) + "xyz",
		},
		{
			name:   "hex run inside non-base64 runes",
			word:   "é" + strings.Repeat("0f", 10) + "é",
			hexOut: []string{strings.Repeat("0f", 10)},
			base64Out: []string{
				strings.Repeat("0f", 10),
			},
		},
	}

	for _, tt := range tests {
		if got := candidates(tt.word, isBase64Rune); !slices.Equal(got, tt.base64Out) {
			t.Errorf("%s: base64 candidates = %q, want %q", tt.name, got, tt.base64Out)
		}

		if got := candidates(tt.word, isHexRune); !slices.Equal(got, tt.hexOut) {
			t.Errorf("%s: hex candidates = %q, want %q", tt.name, got, tt.hexOut)
		}
	}
}

func TestWordSplitting(t *testing.T) {
	tests := []struct {
		line string
		want []string
	}{
		{line: "a b\tc", want: []string{"a", "b", "c"}},
		{line: "a\x1cb\x1dc\x1ed\x1fe", want: []string{"a", "b", "c", "d", "e"}},
		{line: "a\u0085b c　d", want: []string{"a", "b", "c", "d"}},
		{line: "   ", want: []string{}},
	}

	for _, tt := range tests {
		if got := strings.FieldsFunc(tt.line, isPythonSpace); !slices.Equal(got, tt.want) {
			t.Errorf("split(%q) = %q, want %q", tt.line, got, tt.want)
		}
	}
}

func TestNew(t *testing.T) {
	tests := []struct {
		name    string
		wantErr string
		rules   []Rule
	}{
		{name: "no rules"},
		{name: "no group", rules: []Rule{{Name: "a", Pattern: regexp.MustCompile(`x+`)}}},
		{name: "one group", rules: []Rule{{Name: "a", Pattern: regexp.MustCompile(`k=(x+)`)}}},
		{name: "non-capturing groups", rules: []Rule{{Name: "a", Pattern: regexp.MustCompile(`(?:k)=(x+)(?:y)`)}}},
		{
			name:    "two groups",
			rules:   []Rule{{Name: "pair", Pattern: regexp.MustCompile(`(k)=(x+)`)}},
			wantErr: `rule "pair": pattern has 2 capture groups, at most 1 is allowed`,
		},
		{name: "no pattern", rules: []Rule{{Name: "empty"}}, wantErr: `rule "empty": no pattern`},
	}

	for _, tt := range tests {
		_, err := New(cfg.Config{Regex: true}, tt.rules)

		switch {
		case tt.wantErr == "" && err != nil:
			t.Errorf("%s: New() error = %v", tt.name, err)
		case tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr):
			t.Errorf("%s: New() error = %v, want %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestNewDoesNotReorderCallerRules(t *testing.T) {
	rules := []Rule{
		{Name: "b", Pattern: regexp.MustCompile(`b`)},
		{Name: "a", Pattern: regexp.MustCompile(`a`)},
	}

	if _, err := New(cfg.Config{}, rules); err != nil {
		t.Fatal(err)
	}

	if rules[0].Name != "b" {
		t.Errorf("New() reordered the caller's rules")
	}
}

func TestScan(t *testing.T) {
	sigB64 := Signature(highB64, "app/settings.py")

	keyRule := Rule{Name: "key", Pattern: regexp.MustCompile(`key=([a-z]+)`)}
	wordRule := Rule{Name: "word", Pattern: regexp.MustCompile(`not-a-secret`)}

	tests := []struct {
		name  string
		path  string
		rules []Rule
		lines []string
		want  []found
		cfg   cfg.Config
	}{
		{
			name:  "high-entropy base64 reported once",
			cfg:   entropyOnly(),
			lines: []string{"token = " + highB64, "again: " + highB64},
			want:  []found{{Type: EntropyIssue, Match: highB64}},
		},
		{
			name:  "high-entropy hex",
			cfg:   entropyOnly(),
			lines: []string{"digest " + highHex},
			want:  []found{{Type: EntropyIssue, Match: highHex}},
		},
		{
			name:  "entropy at the limit is not flagged",
			cfg:   entropyOnly(),
			lines: []string{limitB64, limitHex},
			want:  []found{},
		},
		{
			name:  "short candidate",
			cfg:   entropyOnly(),
			lines: []string{highB64[:19]},
			want:  []found{},
		},
		{
			name:  "U+001C splits a word",
			cfg:   entropyOnly(),
			lines: []string{highB64[:15] + "\x1c" + highB64[15:]},
			want:  []found{},
		},
		{
			name: "path exclusion short-circuits both scans",
			cfg: cfg.Config{
				Regex: true, Entropy: true,
				ExcludePaths: []*regexp.Regexp{regexp.MustCompile(`^(?:app/)`)},
			},
			rules: []Rule{wordRule},
			lines: []string{"not-a-secret " + highB64},
			want:  []found{},
		},
		{
			name: "path exclusion is start-anchored",
			cfg: cfg.Config{
				Entropy:      true,
				ExcludePaths: []*regexp.Regexp{regexp.MustCompile(`^(?:settings)`)},
			},
			lines: []string{highB64},
			want:  []found{{Type: EntropyIssue, Match: highB64}},
		},
		{
			name:  "raw string in exclude-signatures",
			cfg:   cfg.Config{Entropy: true, ExcludeSignatures: map[string]struct{}{highB64: {}}},
			lines: []string{highB64},
			want:  []found{},
		},
		{
			name:  "signature in exclude-signatures",
			cfg:   cfg.Config{Entropy: true, ExcludeSignatures: map[string]struct{}{sigB64: {}}},
			lines: []string{highB64},
			want:  []found{},
		},
		{
			name: "signature for another path does not exclude",
			cfg: cfg.Config{Entropy: true, ExcludeSignatures: map[string]struct{}{
				Signature(highB64, "other.py"): {},
			}},
			lines: []string{highB64},
			want:  []found{{Type: EntropyIssue, Match: highB64}},
		},
		{
			name:  "regex match in exclude-signatures",
			cfg:   cfg.Config{Regex: true, ExcludeSignatures: map[string]struct{}{"abc": {}}},
			rules: []Rule{keyRule},
			lines: []string{"key=abc key=def"},
			want:  []found{{Type: RegexIssue, Detail: "key", Match: "def"}},
		},
		{
			name:  "entropy pattern matching line and path",
			cfg:   entropyExclusion(`token =`, `settings\.py$`),
			lines: []string{"token = " + highB64},
			want:  []found{},
		},
		{
			name:  "entropy pattern matching only the line",
			cfg:   entropyExclusion(`token =`, `\.env$`),
			lines: []string{"token = " + highB64},
			want:  []found{{Type: EntropyIssue, Match: highB64}},
		},
		{
			name:  "entropy pattern matching only the path",
			cfg:   entropyExclusion(`secret =`, `settings`),
			lines: []string{"token = " + highB64},
			want:  []found{{Type: EntropyIssue, Match: highB64}},
		},
		{
			name:  "entropy pattern with empty path-pattern",
			cfg:   entropyExclusion(`token`, ``),
			lines: []string{"token = " + highB64},
			want:  []found{},
		},
		{
			name:  "entropy pattern applies per line",
			cfg:   entropyExclusion(`token`, ``),
			lines: []string{"token = " + highB64, highHex},
			want:  []found{{Type: EntropyIssue, Match: highHex}},
		},
		{
			name:  "regex group 1",
			cfg:   cfg.Config{Regex: true},
			rules: []Rule{keyRule},
			lines: []string{"key=abc, key=def"},
			want: []found{
				{Type: RegexIssue, Detail: "key", Match: "abc"},
				{Type: RegexIssue, Detail: "key", Match: "def"},
			},
		},
		{
			name:  "regex whole match",
			cfg:   cfg.Config{Regex: true},
			rules: []Rule{wordRule},
			lines: []string{"x not-a-secret y"},
			want:  []found{{Type: RegexIssue, Detail: "word", Match: "not-a-secret"}},
		},
		{
			name:  "regex repeats reported once",
			cfg:   cfg.Config{Regex: true},
			rules: []Rule{wordRule},
			lines: []string{"not-a-secret", "not-a-secret"},
			want:  []found{{Type: RegexIssue, Detail: "word", Match: "not-a-secret"}},
		},
		{
			name:  "regex spans lines",
			cfg:   cfg.Config{Regex: true},
			rules: []Rule{{Name: "pair", Pattern: regexp.MustCompile(`begin\s+(\w+)`)}},
			lines: []string{"begin", "  body"},
			want:  []found{{Type: RegexIssue, Detail: "pair", Match: "body"}},
		},
		{
			name: "regex path pattern matching",
			cfg:  cfg.Config{Regex: true},
			rules: []Rule{{
				Name: "word", Pattern: wordRule.Pattern, PathPattern: regexp.MustCompile(`app/`),
			}},
			lines: []string{"not-a-secret"},
			want:  []found{{Type: RegexIssue, Detail: "word", Match: "not-a-secret"}},
		},
		{
			name: "regex path pattern is start-anchored",
			cfg:  cfg.Config{Regex: true},
			rules: []Rule{{
				Name: "word", Pattern: wordRule.Pattern, PathPattern: regexp.MustCompile(`settings`),
			}},
			lines: []string{"not-a-secret"},
			want:  []found{},
		},
		{
			name:  "regex off",
			cfg:   entropyOnly(),
			rules: []Rule{wordRule},
			lines: []string{"not-a-secret " + highB64},
			want:  []found{{Type: EntropyIssue, Match: highB64}},
		},
		{
			name:  "entropy off",
			cfg:   cfg.Config{Regex: true},
			rules: []Rule{wordRule},
			lines: []string{"not-a-secret " + highB64},
			want:  []found{{Type: RegexIssue, Detail: "word", Match: "not-a-secret"}},
		},
		{
			name:  "both off",
			cfg:   cfg.Config{},
			rules: []Rule{wordRule},
			lines: []string{"not-a-secret " + highB64},
			want:  []found{},
		},
		{
			name:  "order: rules by name, then entropy by line",
			cfg:   cfg.Config{Regex: true, Entropy: true},
			rules: []Rule{wordRule, keyRule},
			lines: []string{highHex + " not-a-secret", "key=abc " + highB64},
			want: []found{
				{Type: RegexIssue, Detail: "key", Match: "abc"},
				{Type: RegexIssue, Detail: "word", Match: "not-a-secret"},
				{Type: EntropyIssue, Match: highHex},
				{Type: EntropyIssue, Match: highB64},
			},
		},
	}

	for _, tt := range tests {
		e, err := New(tt.cfg, tt.rules)
		if err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}

		path := tt.path
		if path == "" {
			path = "app/settings.py"
		}

		got := summarize(e.Scan(Chunk{Path: path, Lines: tt.lines}))
		if !slices.Equal(got, tt.want) {
			t.Errorf("%s: Scan() = %+v, want %+v", tt.name, got, tt.want)
		}
	}
}

func entropyExclusion(pattern, pathPattern string) cfg.Config {
	return cfg.Config{
		Entropy: true,
		ExcludeEntropyPatterns: []cfg.EntropyExclusion{{
			Pattern:     regexp.MustCompile(pattern),
			PathPattern: regexp.MustCompile(pathPattern),
		}},
	}
}

func TestScanIssueFields(t *testing.T) {
	e, err := New(cfg.Config{Regex: true, Entropy: true}, []Rule{
		{Name: "word", Pattern: regexp.MustCompile(`not-a-secret`)},
	})
	if err != nil {
		t.Fatal(err)
	}

	c := Chunk{Path: "dir/für.txt", Lines: []string{"not-a-secret", highB64}}

	issues := e.Scan(c)
	if len(issues) != 2 {
		t.Fatalf("Scan() returned %d issues, want 2", len(issues))
	}

	for _, i := range issues {
		if i.Path != c.Path {
			t.Errorf("%s: Path = %q, want %q", i.Match, i.Path, c.Path)
		}

		if want := Signature(i.Match, c.Path); i.Signature != want {
			t.Errorf("%s: Signature = %s, want %s", i.Match, i.Signature, want)
		}

		if !reflect.DeepEqual(i.Chunk, c) {
			t.Errorf("%s: Chunk = %+v, want %+v", i.Match, i.Chunk, c)
		}
	}
}

// TestScanConcurrent shares one Engine across goroutines; run with -race. Every
// call must return the same issues in the same order.
func TestScanConcurrent(t *testing.T) {
	e, err := New(cfg.Config{Regex: true, Entropy: true}, []Rule{
		{Name: "word", Pattern: regexp.MustCompile(`not-a-secret`)},
		{Name: "key", Pattern: regexp.MustCompile(`key=([a-z]+)`)},
	})
	if err != nil {
		t.Fatal(err)
	}

	c := Chunk{Path: "a.txt", Lines: []string{"key=abc not-a-secret", highB64 + " " + highHex}}
	want := e.Scan(c)

	var wg sync.WaitGroup

	for range 16 {
		wg.Go(func() {
			for range 50 {
				if got := e.Scan(c); !reflect.DeepEqual(got, want) {
					t.Errorf("Scan() = %+v, want %+v", got, want)

					return
				}
			}
		})
	}

	wg.Wait()
}
