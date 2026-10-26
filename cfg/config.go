// Package cfg loads gelato's configuration from the `[tool.tartufo]` table of
// tartufo-compatible TOML files, so existing tartufo users keep their config.
package cfg

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type (
	// Config is the validated configuration merged from the target
	// repository's config file and any explicitly given config files.
	Config struct {
		// ExcludeSignatures is the set of finding signatures to suppress.
		ExcludeSignatures map[string]struct{}
		// RepoPath is the absolute path of the repository to scan.
		RepoPath string
		// ExcludePaths are start-anchored patterns; a file whose path matches
		// any of them is not scanned.
		ExcludePaths []*regexp.Regexp
		// ExcludeEntropyPatterns suppress entropy findings on matching lines.
		ExcludeEntropyPatterns []EntropyExclusion
		// Regex enables rule-based scanning.
		Regex bool
		// Entropy enables high-entropy string scanning.
		Entropy bool
	}

	// EntropyExclusion suppresses entropy findings whose text matches Pattern
	// in files whose path matches PathPattern. Both patterns are unanchored.
	EntropyExclusion struct {
		Pattern     *regexp.Regexp
		PathPattern *regexp.Regexp
		Reason      string
	}

	// layer holds the validated settings of a single config file. Scalars are
	// nil when the file doesn't set them.
	layer struct {
		repoPath          *string
		regex             *bool
		entropy           *bool
		excludePaths      []*regexp.Regexp
		excludeEntropy    []EntropyExclusion
		excludeSignatures []string
	}
)

// Keys of the `[tool.tartufo]` table and of its list entries, in their
// canonical spelling.
const (
	keyRepoPath               = "repo-path"
	keyRegex                  = "regex"
	keyEntropy                = "entropy"
	keyExcludePathPatterns    = "exclude-path-patterns"
	keyExcludeEntropyPatterns = "exclude-entropy-patterns"
	keyExcludeSignatures      = "exclude-signatures"
	keyPathPattern            = "path-pattern"
	keyPattern                = "pattern"
	keyReason                 = "reason"
	keySignature              = "signature"
)

// Load reads the explicit config files (as given with `--config`, relative to
// the working directory) and the target config file discovered in repoDir,
// then validates and merges them. An empty repoDir means the repository path
// comes from the explicit files' `repo-path`, defaulting to ".".
func Load(explicit []string, repoDir string) (Config, error) {
	explicitLayers := make([]layer, 0, len(explicit))
	explicitInfos := make([]fs.FileInfo, 0, len(explicit))

	for _, path := range explicit {
		l, info, found, err := readFile(path)
		if err != nil {
			return Config{}, err
		}

		if !found {
			return Config{}, fmt.Errorf("%s: no [tool.tartufo] table", path)
		}

		explicitLayers = append(explicitLayers, l)
		explicitInfos = append(explicitInfos, info)
	}

	if repoDir == "" {
		repoDir = "."

		for _, l := range explicitLayers {
			if l.repoPath != nil {
				repoDir = *l.repoPath
			}
		}
	}

	absRepoDir, err := filepath.Abs(repoDir)
	if err != nil {
		return Config{}, fmt.Errorf("resolving repository path %q: %s", repoDir, err.Error())
	}

	target, found, err := readTarget(repoDir, explicitInfos)
	if err != nil {
		return Config{}, err
	}

	layers := explicitLayers
	if found {
		layers = append([]layer{target}, explicitLayers...)
	}

	return merge(absRepoDir, layers)
}

// readTarget reads the first existing target config file in repoDir. found is
// false when there is none, when it has no `[tool.tartufo]` table, or when it
// is one of the explicit files (which were already read).
func readTarget(repoDir string, explicit []fs.FileInfo) (l layer, found bool, err error) {
	for _, name := range []string{"tartufo.toml", "pyproject.toml"} {
		path := filepath.Join(repoDir, name)

		info, err := os.Stat(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return layer{}, false, fmt.Errorf("reading config: %s", err.Error())
		}

		for _, e := range explicit {
			if os.SameFile(info, e) {
				return layer{}, false, nil
			}
		}

		l, _, found, err := readFile(path)

		return l, found, err
	}

	return layer{}, false, nil
}

// readFile parses the `[tool.tartufo]` table of the TOML file at path. found
// is false when the file has no such table.
func readFile(path string) (l layer, info fs.FileInfo, found bool, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return layer{}, nil, false, fmt.Errorf("reading config: %s", err.Error())
	}

	info, err = os.Stat(path)
	if err != nil {
		return layer{}, nil, false, fmt.Errorf("reading config: %s", err.Error())
	}

	var doc map[string]any
	if err = toml.Unmarshal(data, &doc); err != nil {
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			row, col := decodeErr.Position()

			return layer{}, nil, false, fmt.Errorf("%s:%d:%d: %s", path, row, col, decodeErr.Error())
		}

		return layer{}, nil, false, fmt.Errorf("%s: %s", path, err.Error())
	}

	rawTool, ok := doc["tool"]
	if !ok {
		return layer{}, info, false, nil
	}

	tool, ok := rawTool.(map[string]any)
	if !ok {
		return layer{}, nil, false, fmt.Errorf("%s: tool: expected a table, got %s", path, typeName(rawTool))
	}

	rawTable, ok := tool["tartufo"]
	if !ok {
		return layer{}, info, false, nil
	}

	table, ok := rawTable.(map[string]any)
	if !ok {
		return layer{}, nil, false, fmt.Errorf("%s: tool.tartufo: expected a table, got %s", path, typeName(rawTable))
	}

	l, err = parseTable(table)
	if err != nil {
		return layer{}, nil, false, fmt.Errorf("%s: %s", path, err.Error())
	}

	return l, info, true, nil
}

// parseTable validates a `[tool.tartufo]` table. Top-level keys accept `_` in
// place of `-`, as tartufo does; keys inside list entries must be spelled
// exactly, since tartufo looks them up verbatim.
func parseTable(table map[string]any) (layer, error) {
	var l layer

	seen := make(map[string]string, len(table))

	for _, rawKey := range slices.Sorted(maps.Keys(table)) {
		value := table[rawKey]

		key := strings.ReplaceAll(rawKey, "_", "-")
		if other, dup := seen[key]; dup {
			return layer{}, fmt.Errorf("%s: duplicates key %s", rawKey, other)
		}

		seen[key] = rawKey

		var err error

		switch key {
		case keyRepoPath:
			l.repoPath, err = optional(asString(key, value))
		case keyRegex:
			l.regex, err = optional(asBool(key, value))
		case keyEntropy:
			l.entropy, err = optional(asBool(key, value))
		case keyExcludePathPatterns:
			l.excludePaths, err = parsePathPatterns(key, value)
		case keyExcludeEntropyPatterns:
			l.excludeEntropy, err = parseEntropyPatterns(key, value)
		case keyExcludeSignatures:
			l.excludeSignatures, err = parseSignatures(key, value)
		default:
			err = fmt.Errorf("%s: unknown key", rawKey)
		}

		if err != nil {
			return layer{}, err
		}
	}

	return l, nil
}

// parsePathPatterns compiles `exclude-path-patterns` start-anchored, skipping
// blank and `#`-prefixed entries.
func parsePathPatterns(key string, value any) ([]*regexp.Regexp, error) {
	entries, err := asTables(key, value, []string{keyPathPattern}, []string{keyReason})
	if err != nil {
		return nil, err
	}

	patterns := make([]*regexp.Regexp, 0, len(entries))

	for i, entry := range entries {
		at := fmt.Sprintf("%s[%d].%s", key, i, keyPathPattern)

		p, err := asString(at, entry[keyPathPattern])
		if err != nil {
			return nil, err
		}

		p = strings.TrimSpace(p)
		if p == "" || strings.HasPrefix(p, "#") {
			continue
		}

		// Compile the pattern on its own first so the error quotes what the
		// user wrote, not the anchored wrapper.
		if _, err = regexp.Compile(p); err != nil {
			return nil, fmt.Errorf("%s: %s", at, err.Error())
		}

		re, err := regexp.Compile("^(?:" + p + ")")
		if err != nil {
			return nil, fmt.Errorf("%s: %s", at, err.Error())
		}

		patterns = append(patterns, re)
	}

	return patterns, nil
}

// parseEntropyPatterns compiles `exclude-entropy-patterns` unanchored.
func parseEntropyPatterns(key string, value any) ([]EntropyExclusion, error) {
	entries, err := asTables(key, value, []string{keyPattern}, []string{keyPathPattern, keyReason})
	if err != nil {
		return nil, err
	}

	exclusions := make([]EntropyExclusion, 0, len(entries))

	for i, entry := range entries {
		at := fmt.Sprintf("%s[%d]", key, i)

		var exclusion EntropyExclusion

		if exclusion.Pattern, err = asRegexp(at+"."+keyPattern, entry[keyPattern]); err != nil {
			return nil, err
		}

		var pathPattern any = ""
		if v, ok := entry[keyPathPattern]; ok {
			pathPattern = v
		}

		if exclusion.PathPattern, err = asRegexp(at+"."+keyPathPattern, pathPattern); err != nil {
			return nil, err
		}

		if exclusion.Reason, err = optionalString(at, entry, keyReason); err != nil {
			return nil, err
		}

		exclusions = append(exclusions, exclusion)
	}

	return exclusions, nil
}

// parseSignatures reads `exclude-signatures`. Duplicates are allowed.
func parseSignatures(key string, value any) ([]string, error) {
	entries, err := asTables(key, value, []string{keySignature}, []string{keyReason})
	if err != nil {
		return nil, err
	}

	signatures := make([]string, 0, len(entries))

	for i, entry := range entries {
		s, err := asString(fmt.Sprintf("%s[%d].%s", key, i, keySignature), entry[keySignature])
		if err != nil {
			return nil, err
		}

		signatures = append(signatures, s)
	}

	return signatures, nil
}

// asTables checks that value is an array of tables, each holding all required
// keys, no keys outside required and optional, and a string `reason` if any.
func asTables(key string, value any, required, optional []string) ([]map[string]any, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: expected an array of tables, got %s", key, typeName(value))
	}

	allowed := make(map[string]bool, len(required)+len(optional))
	for _, k := range slices.Concat(required, optional) {
		allowed[k] = true
	}

	tables := make([]map[string]any, 0, len(items))

	for i, item := range items {
		at := fmt.Sprintf("%s[%d]", key, i)

		table, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: expected a table, got %s", at, typeName(item))
		}

		for _, k := range required {
			if _, ok := table[k]; !ok {
				return nil, fmt.Errorf("%s: missing required key %s", at, k)
			}
		}

		for _, k := range slices.Sorted(maps.Keys(table)) {
			if !allowed[k] {
				return nil, fmt.Errorf("%s.%s: unknown key", at, k)
			}
		}

		if _, err := optionalString(at, table, keyReason); err != nil {
			return nil, err
		}

		tables = append(tables, table)
	}

	return tables, nil
}

// merge applies layers in order: list values concatenate, and scalars from
// later layers override earlier ones. Repository paths were resolved by Load.
func merge(repoPath string, layers []layer) (Config, error) {
	c := Config{
		RepoPath:          repoPath,
		Regex:             true,
		Entropy:           true,
		ExcludeSignatures: make(map[string]struct{}),
	}

	for _, l := range layers {
		if l.regex != nil {
			c.Regex = *l.regex
		}

		if l.entropy != nil {
			c.Entropy = *l.entropy
		}

		c.ExcludePaths = append(c.ExcludePaths, l.excludePaths...)
		c.ExcludeEntropyPatterns = append(c.ExcludeEntropyPatterns, l.excludeEntropy...)

		for _, s := range l.excludeSignatures {
			c.ExcludeSignatures[s] = struct{}{}
		}
	}

	if !c.Regex && !c.Entropy {
		return Config{}, errors.New("no analysis requested: regex and entropy are both false")
	}

	return c, nil
}

func asString(at string, value any) (string, error) {
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s: expected a string, got %s", at, typeName(value))
	}

	return s, nil
}

func asBool(at string, value any) (bool, error) {
	b, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("%s: expected a boolean, got %s", at, typeName(value))
	}

	return b, nil
}

func asRegexp(at string, value any) (*regexp.Regexp, error) {
	s, err := asString(at, value)
	if err != nil {
		return nil, err
	}

	re, err := regexp.Compile(s)
	if err != nil {
		return nil, fmt.Errorf("%s: %s", at, err.Error())
	}

	return re, nil
}

// optionalString returns the string under key in the table found at at, or
// "" if the key is absent.
func optionalString(at string, table map[string]any, key string) (string, error) {
	v, ok := table[key]
	if !ok {
		return "", nil
	}

	return asString(at+"."+key, v)
}

// optional adapts a scalar parser's result to a "set in this file" pointer.
func optional[T any](v T, err error) (*T, error) {
	if err != nil {
		return nil, err
	}

	return &v, nil
}

// typeName names a decoded TOML value's type for error messages.
func typeName(value any) string {
	switch value.(type) {
	case string:
		return "string"
	case bool:
		return "boolean"
	case int64:
		return "integer"
	case float64:
		return "float"
	case []any:
		return "array"
	case map[string]any:
		return "table"
	default:
		return "date/time"
	}
}
