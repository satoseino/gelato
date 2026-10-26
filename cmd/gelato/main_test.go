package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/satoseino/gelato/command"
)

type (
	// findingsCLI is a root command line whose only command reports findings.
	findingsCLI struct {
		Find findingsCommand `cmd:""`
	}

	findingsCommand struct{}
)

func TestRun(t *testing.T) {
	help, err := os.ReadFile(filepath.Join("testdata", "help.txt"))
	if err != nil {
		t.Fatal(err)
	}

	invalid, err := filepath.Abs(filepath.Join("testdata", "invalid.toml"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
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
			wantStdout: string(help),
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
			name:       "invalid config",
			args:       []string{"--config", invalid, "pre-commit"},
			wantCode:   2,
			wantStderr: "gelato: error: " + invalid + ": exclude-signatures[0].note: unknown key\n",
		},
		{
			name:       "pre-commit not implemented",
			args:       []string{"pre-commit"},
			wantCode:   2,
			wantStderr: "gelato: error: pre-commit: not implemented\n",
		},
		{
			name:       "scan not implemented",
			args:       []string{"scan"},
			wantCode:   2,
			wantStderr: "gelato: error: scan: not implemented\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Keep the commands away from any config in this repository.
			t.Chdir(t.TempDir())

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
