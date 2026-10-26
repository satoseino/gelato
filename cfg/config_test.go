package cfg_test

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/satoseino/gelato/cfg"
)

// summary is a comparable view of a cfg.Config.
type summary struct {
	RepoPath               string
	ExcludePaths           []string
	ExcludeEntropyPatterns []string
	ExcludeSignatures      []string
	Regex                  bool
	Entropy                bool
}

func summarize(c cfg.Config) summary {
	s := summary{
		RepoPath: c.RepoPath,
		Regex:    c.Regex,
		Entropy:  c.Entropy,
	}

	for _, re := range c.ExcludePaths {
		s.ExcludePaths = append(s.ExcludePaths, re.String())
	}

	for _, e := range c.ExcludeEntropyPatterns {
		s.ExcludeEntropyPatterns = append(s.ExcludeEntropyPatterns,
			e.Pattern.String()+" | "+e.PathPattern.String()+" | "+e.Reason)
	}

	for sig := range c.ExcludeSignatures {
		s.ExcludeSignatures = append(s.ExcludeSignatures, sig)
	}

	slices.Sort(s.ExcludeSignatures)

	return s
}

// setup changes into a fresh temp dir and copies fixtures into it, keyed by
// destination path relative to that dir. It returns the dir.
func setup(t *testing.T, files map[string]string) string {
	t.Helper()

	src, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}

	t.Chdir(t.TempDir())

	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for dst, fixture := range files {
		data, err := os.ReadFile(filepath.Join(src, fixture))
		if err != nil {
			t.Fatal(err)
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(dst, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func TestLoad(t *testing.T) {
	jarvisPaths := []string{`^(?:uv\.lock$)`, `^(?:.*package-lock\.json$)`, `^(?:.*\.svg$)`}
	jarvisEntropy := []string{
		`PRIVATE_KEY=.* | \.env\.example$ | placeholder`,
		`.*TENANTID123.* | (app|lib)/tests/.* | public tenant ID`,
	}
	jarvisSignatures := []string{
		"1111111111111111111111111111111111111111111111111111111111111111",
		"2222222222222222222222222222222222222222222222222222222222222222",
	}

	tests := []struct {
		files    map[string]string
		name     string
		repoDir  string
		explicit []string
		want     summary
	}{
		{
			name:    "no config files",
			repoDir: ".",
			want:    summary{RepoPath: ".", Regex: true, Entropy: true},
		},
		{
			name:    "tartufo.toml discovered",
			files:   map[string]string{"repo/tartufo.toml": "jarvis-like.toml"},
			repoDir: "repo",
			want: summary{
				RepoPath:               "repo",
				Regex:                  true,
				Entropy:                true,
				ExcludePaths:           jarvisPaths,
				ExcludeEntropyPatterns: jarvisEntropy,
				ExcludeSignatures:      jarvisSignatures,
			},
		},
		{
			name:    "pyproject.toml used when tartufo.toml is absent",
			files:   map[string]string{"pyproject.toml": "pyproject.toml"},
			repoDir: ".",
			want:    summary{RepoPath: ".", Regex: true, Entropy: true, ExcludeSignatures: []string{"pyproject-sig"}},
		},
		{
			name: "tartufo.toml preferred over pyproject.toml",
			files: map[string]string{
				"tartufo.toml":   "extra.toml",
				"pyproject.toml": "pyproject.toml",
			},
			repoDir: ".",
			want: summary{
				RepoPath:          ".",
				Regex:             true,
				ExcludePaths:      []string{`^(?:vendor/)`},
				ExcludeSignatures: []string{"extra-sig"},
			},
		},
		{
			name: "tartufo.toml without table means no target config",
			files: map[string]string{
				"tartufo.toml":   "no-table.toml",
				"pyproject.toml": "pyproject.toml",
			},
			repoDir: ".",
			want:    summary{RepoPath: ".", Regex: true, Entropy: true},
		},
		{
			name: "explicit file merged after target file",
			files: map[string]string{
				"repo/tartufo.toml": "jarvis-like.toml",
				"extra.toml":        "extra.toml",
			},
			explicit: []string{"extra.toml"},
			repoDir:  "repo",
			want: summary{
				RepoPath:               "repo",
				Regex:                  true,
				Entropy:                false,
				ExcludePaths:           append(slices.Clone(jarvisPaths), `^(?:vendor/)`),
				ExcludeEntropyPatterns: jarvisEntropy,
				ExcludeSignatures:      append(slices.Clone(jarvisSignatures), "extra-sig"),
			},
		},
		{
			name: "explicit files merged in order",
			files: map[string]string{
				"extra.toml":    "extra.toml",
				"override.toml": "override.toml",
			},
			explicit: []string{"extra.toml", "override.toml"},
			want: summary{
				RepoPath:          "from-override",
				Regex:             false,
				Entropy:           true,
				ExcludePaths:      []string{`^(?:vendor/)`},
				ExcludeSignatures: []string{"extra-sig", "override-sig"},
			},
		},
		{
			name:     "explicit file that is also the target file is read once",
			files:    map[string]string{"repo/tartufo.toml": "jarvis-like.toml"},
			explicit: []string{"repo/tartufo.toml"},
			repoDir:  "repo",
			want: summary{
				RepoPath:               "repo",
				Regex:                  true,
				Entropy:                true,
				ExcludePaths:           jarvisPaths,
				ExcludeEntropyPatterns: jarvisEntropy,
				ExcludeSignatures:      jarvisSignatures,
			},
		},
		{
			name: "repo-path from explicit file locates target file",
			files: map[string]string{
				"extra.toml":              "extra.toml",
				"from-extra/tartufo.toml": "pyproject.toml",
			},
			explicit: []string{"extra.toml"},
			want: summary{
				RepoPath:          "from-extra",
				Regex:             true,
				ExcludePaths:      []string{`^(?:vendor/)`},
				ExcludeSignatures: []string{"extra-sig", "pyproject-sig"},
			},
		},
		{
			name:     "repo-path argument wins over explicit file",
			files:    map[string]string{"extra.toml": "extra.toml"},
			explicit: []string{"extra.toml"},
			repoDir:  "repo",
			want: summary{
				RepoPath:          "repo",
				Regex:             true,
				ExcludePaths:      []string{`^(?:vendor/)`},
				ExcludeSignatures: []string{"extra-sig"},
			},
		},
		{
			name:  "repo-path in target file has no effect",
			files: map[string]string{"tartufo.toml": "override.toml"},
			want: summary{
				RepoPath:          ".",
				Regex:             false,
				Entropy:           true,
				ExcludeSignatures: []string{"override-sig"},
			},
		},
		{
			name:     "underscores normalized in top-level keys",
			files:    map[string]string{"underscore-keys.toml": "underscore-keys.toml"},
			explicit: []string{"underscore-keys.toml"},
			want: summary{
				RepoPath:               "from-underscore",
				Regex:                  true,
				Entropy:                true,
				ExcludePaths:           []string{`^(?:docs/)`},
				ExcludeEntropyPatterns: []string{"abc |  | "},
				ExcludeSignatures:      []string{"underscore-sig"},
			},
		},
		{
			name:    "array-of-tables syntax",
			files:   map[string]string{"tartufo.toml": "array-of-tables.toml"},
			repoDir: ".",
			want:    summary{RepoPath: ".", Regex: true, Entropy: true, ExcludeSignatures: []string{"aot-sig"}},
		},
		{
			name:    "blank and commented path patterns skipped",
			files:   map[string]string{"tartufo.toml": "path-patterns.toml"},
			repoDir: ".",
			want: summary{
				RepoPath:     ".",
				Regex:        true,
				Entropy:      true,
				ExcludePaths: []string{`^(?:uv\.lock$)`, `^(?:docs/)`, `^(?:a|b)`},
			},
		},
		{
			name:    "regex disabled",
			files:   map[string]string{"tartufo.toml": "regex-off.toml"},
			repoDir: ".",
			want:    summary{RepoPath: ".", Regex: false, Entropy: true},
		},
		{
			name:    "entropy disabled",
			files:   map[string]string{"tartufo.toml": "entropy-off.toml"},
			repoDir: ".",
			want:    summary{RepoPath: ".", Regex: true, Entropy: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := setup(t, tt.files)

			c, err := cfg.Load(tt.explicit, tt.repoDir)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			tt.want.RepoPath = filepath.Join(dir, tt.want.RepoPath)

			if got := summarize(c); !equal(got, tt.want) {
				t.Errorf("Load:\n got %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func equal(a, b summary) bool {
	return a.RepoPath == b.RepoPath &&
		a.Regex == b.Regex &&
		a.Entropy == b.Entropy &&
		slices.Equal(a.ExcludePaths, b.ExcludePaths) &&
		slices.Equal(a.ExcludeEntropyPatterns, b.ExcludeEntropyPatterns) &&
		slices.Equal(a.ExcludeSignatures, b.ExcludeSignatures)
}

// TestLoadInvalid loads each testdata/invalid/*.toml both as an explicit file
// and as the target file, and compares the error with the matching .err file,
// where {file} stands for the config file's path.
func TestLoadInvalid(t *testing.T) {
	fixtures, err := filepath.Glob(filepath.Join("testdata", "invalid", "*.toml"))
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range fixtures {
		golden, err := os.ReadFile(strings.TrimSuffix(path, ".toml") + ".err")
		if err != nil {
			t.Fatal(err)
		}

		fixture, err := filepath.Rel("testdata", path)
		if err != nil {
			t.Fatal(err)
		}

		wantFor := func(file string) string {
			return strings.ReplaceAll(strings.TrimSpace(string(golden)), "{file}", file)
		}

		t.Run(fixture+"/explicit", func(t *testing.T) {
			setup(t, map[string]string{"bad.toml": fixture})

			_, err := cfg.Load([]string{"bad.toml"}, ".")
			if got, want := errString(err), wantFor("bad.toml"); got != want {
				t.Errorf("Load error:\n got %q\nwant %q", got, want)
			}
		})

		t.Run(fixture+"/target", func(t *testing.T) {
			setup(t, map[string]string{"repo/tartufo.toml": fixture})

			_, err := cfg.Load(nil, "repo")
			if got, want := errString(err), wantFor(filepath.Join("repo", "tartufo.toml")); got != want {
				t.Errorf("Load error:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestLoadExplicitErrors(t *testing.T) {
	tests := []struct {
		files    map[string]string
		name     string
		want     string
		explicit []string
	}{
		{
			name:     "missing file",
			explicit: []string{"missing.toml"},
			want:     "reading config: open missing.toml: no such file or directory",
		},
		{
			name:     "no table",
			files:    map[string]string{"no-table.toml": "no-table.toml"},
			explicit: []string{"no-table.toml"},
			want:     "no-table.toml: no [tool.tartufo] table",
		},
		{
			name: "merged scalars disable all analysis",
			files: map[string]string{
				"regex-off.toml":   "regex-off.toml",
				"entropy-off.toml": "entropy-off.toml",
			},
			explicit: []string{"regex-off.toml", "entropy-off.toml"},
			want:     "no analysis requested: regex and entropy are both false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setup(t, tt.files)

			_, err := cfg.Load(tt.explicit, ".")
			if got := errString(err); got != tt.want {
				t.Errorf("Load error:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}

	return err.Error()
}

func TestExcludePathsAreStartAnchored(t *testing.T) {
	setup(t, map[string]string{"tartufo.toml": "path-patterns.toml"})

	c, err := cfg.Load(nil, ".")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want bool
	}{
		{path: "uv.lock", want: true},
		{path: "sub/uv.lock", want: false},
		{path: "uv.lock.bak", want: false},
		{path: "docs/index.md", want: true},
		{path: "src/docs/index.md", want: false},
		{path: "a", want: true},
		{path: "b/c", want: true},
		{path: "xb", want: false},
	}

	for _, tt := range tests {
		got := slices.ContainsFunc(c.ExcludePaths, func(re *regexp.Regexp) bool {
			return re.MatchString(tt.path)
		})
		if got != tt.want {
			t.Errorf("excluded(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestExcludeEntropyPatternsSearch(t *testing.T) {
	setup(t, map[string]string{"tartufo.toml": "entropy-patterns.toml"})

	c, err := cfg.Load(nil, ".")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		line, path string
		want       bool
	}{
		{line: "export KEY=abc", path: "config/.env", want: true},
		{line: "KEY=abc", path: ".env", want: true},
		{line: "KEY=abc", path: ".env.bak", want: false},
		{line: "export KEY=abc", path: "main.go", want: false},
		{line: "the TOKEN here", path: "any/where.txt", want: true},
		{line: "nothing", path: ".env", want: false},
	}

	for _, tt := range tests {
		got := slices.ContainsFunc(c.ExcludeEntropyPatterns, func(e cfg.EntropyExclusion) bool {
			return e.Pattern.MatchString(tt.line) && e.PathPattern.MatchString(tt.path)
		})
		if got != tt.want {
			t.Errorf("excluded(%q, %q) = %v, want %v", tt.line, tt.path, got, tt.want)
		}
	}
}
