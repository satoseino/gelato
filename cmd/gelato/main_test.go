package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/satoseino/gelato/command"
	"github.com/satoseino/gelato/internal/gittest"
)

type (
	// findingsCLI is a root command line whose only command reports findings.
	findingsCLI struct {
		Find findingsCommand `cmd:""`
	}

	findingsCommand struct{}
)

func TestRun(t *testing.T) {
	help := readTestdata(t, "help.txt")
	helpScan := readTestdata(t, "help-scan.txt")
	helpPreCommit := readTestdata(t, "help-pre-commit.txt")

	testdata, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}

	invalid := filepath.Join(testdata, "invalid.toml")

	tests := []struct {
		files      map[string]string
		name       string
		wantStdout string
		wantStderr string
		args       []string
		wantCode   int
	}{
		{
			name:       "help",
			args:       []string{"--help"},
			wantCode:   0,
			wantStdout: help,
		},
		{
			name:       "scan help",
			args:       []string{"scan", "--help"},
			wantCode:   0,
			wantStdout: helpScan,
		},
		{
			name:       "pre-commit help",
			args:       []string{"pre-commit", "--help"},
			wantCode:   0,
			wantStdout: helpPreCommit,
		},
		{
			name:       "version",
			args:       []string{"--version"},
			wantCode:   0,
			wantStdout: "dev\n",
		},
		{
			name:       "version short flag",
			args:       []string{"-V"},
			wantCode:   0,
			wantStdout: "dev\n",
		},
		{
			name:       "unknown flag",
			args:       []string{"--bogus"},
			wantCode:   2,
			wantStderr: "gelato: error: unknown flag --bogus\n",
		},
		{
			name:       "invalid output format",
			args:       []string{"--output-format=xml", "pre-commit"},
			wantCode:   2,
			wantStderr: `gelato: error: --output-format must be one of "text","json" but got "xml"` + "\n",
		},
		{
			name:       "invalid config with JSON output",
			args:       []string{"--output-format=json", "--config", invalid, "pre-commit"},
			wantCode:   2,
			wantStderr: "gelato: error: " + invalid + ": exclude-signatures[0].note: unknown key\n",
		},
		{
			name:       "invalid config",
			args:       []string{"--config", invalid, "pre-commit"},
			wantCode:   2,
			wantStderr: "gelato: error: " + invalid + ": exclude-signatures[0].note: unknown key\n",
		},
		{
			name:       "pre-commit loads tartufo.toml from the current directory",
			files:      map[string]string{"tartufo.toml": "invalid.toml"},
			args:       []string{"pre-commit"},
			wantCode:   2,
			wantStderr: "gelato: error: tartufo.toml: exclude-signatures[0].note: unknown key\n",
		},
		{
			name:       "scan loads tartufo.toml from the repo-path argument",
			files:      map[string]string{"repo/tartufo.toml": "invalid.toml"},
			args:       []string{"scan", "repo"},
			wantCode:   2,
			wantStderr: "gelato: error: repo/tartufo.toml: exclude-signatures[0].note: unknown key\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Keep the commands away from any config in this repository.
			t.Chdir(t.TempDir())

			for dst, fixture := range tt.files {
				data, err := os.ReadFile(filepath.Join(testdata, fixture))
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

			var stdout, stderr bytes.Buffer

			code := run(tt.args, &stdout, &stderr)

			if code != tt.wantCode {
				t.Errorf("exit code = %d, want %d", code, tt.wantCode)
			}

			if got := stdout.String(); got != tt.wantStdout {
				t.Errorf("stdout:\n got %q\nwant %q", got, tt.wantStdout)
			}

			if got := stderr.String(); got != tt.wantStderr {
				t.Errorf("stderr:\n got %q\nwant %q", got, tt.wantStderr)
			}
		})
	}
}

func readTestdata(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return string(data)
}

func (findingsCommand) Run() error {
	return fmt.Errorf("3 issues: %w", command.ErrFindings)
}

func TestExecuteFindings(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if code := execute(&findingsCLI{}, []string{"find"}, &stdout, &stderr); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}

	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("unexpected output: stdout %q, stderr %q", stdout.String(), stderr.String())
	}
}

// TestRunPreCommit checks that the pre-commit report goes to run's stdout.
func TestRunPreCommit(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "one\n")
	repo.Git("add", "a.txt")
	t.Chdir(repo.Dir)

	var stdout, stderr bytes.Buffer

	if code := run([]string{"pre-commit", "--no-color"}, &stdout, &stderr); code != 0 {
		t.Errorf("exit code = %d, want 0; stderr %q", code, stderr.String())
	}

	if want := "All clear. No secrets detected.\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

// TestRunScan checks that the scan report goes to run's stdout.
func TestRunScan(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "one\n")
	repo.Commit("init")

	var stdout, stderr bytes.Buffer

	if code := run([]string{"scan", "--no-color", repo.Dir}, &stdout, &stderr); code != 0 {
		t.Errorf("exit code = %d, want 0; stderr %q", code, stderr.String())
	}

	if want := "All clear. No secrets detected.\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

// TestRunScanJSON checks that the JSON report of a clean scan goes to run's
// stdout as one document with an empty found_issues.
func TestRunScanJSON(t *testing.T) {
	repo := gittest.New(t)
	repo.Write("a.txt", "one\n")
	repo.Commit("init")

	var stdout, stderr bytes.Buffer

	if code := run([]string{"scan", "--output-format=json", repo.Dir}, &stdout, &stderr); code != 0 {
		t.Errorf("exit code = %d, want 0; stderr %q", code, stderr.String())
	}

	var doc map[string][]any
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		t.Fatalf("stdout %q is not JSON: %v", stdout.String(), err)
	}

	if issues, ok := doc["found_issues"]; !ok || issues == nil || len(issues) != 0 {
		t.Errorf("stdout = %q, want an empty found_issues", stdout.String())
	}
}
