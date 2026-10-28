// Command gelato scans a git repository for secrets.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/alecthomas/kong"

	"github.com/satoseino/gelato/command"
)

type (
	// cli is the root command line.
	cli struct { //nolint:govet // fieldalignment: kong lists commands and flags in field order.
		PreCommit command.PreCommit `cmd:"" help:"Scan lines added in staged changes." name:"pre-commit"`
		Scan      command.Scan      `cmd:"" help:"Scan lines added across the repository's history." name:"scan"`

		command.Globals

		Version kong.VersionFlag `help:"Print the version and exit." short:"V"`
	}

	// exit is panicked by kong's exit hook so that --help and --version stop
	// parsing and return from execute instead of ending the process.
	exit int
)

// devVersion is the version of a build that has no version stamped into it.
const devVersion = "dev"

// version is set at release time with -ldflags "-X main.version=...".
var version = devVersion

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	var root cli

	return execute(&root, args, stdout, stderr,
		kong.Bind(&root.Globals),
		kong.BindTo(stdout, (*io.Writer)(nil)),
	)
}

// execute parses args into root, runs the selected command, and returns the
// process exit code.
func execute(root any, args []string, stdout, stderr io.Writer, options ...kong.Option) (code int) {
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(exit)
			if !ok {
				panic(r)
			}

			code = int(e)
		}
	}()

	info, ok := debug.ReadBuildInfo()

	options = append(options,
		kong.Name("gelato"),
		kong.Description("Scan a git repository for secrets."),
		kong.Vars{"version": resolveVersion(version, info, ok)},
		kong.Writers(stdout, stderr),
		kong.Exit(func(code int) { panic(exit(code)) }),
	)

	parser, err := kong.New(root, options...)
	if err != nil {
		return exitCode(stderr, err)
	}

	ctx, err := parser.Parse(args)
	if err != nil {
		return exitCode(stderr, err)
	}

	return exitCode(stderr, ctx.Run())
}

// resolveVersion returns stamped, the version set with -ldflags, unless it is
// devVersion. Builds without -ldflags, such as pre-commit's `go install`, fall
// back to the main module version that Go stamps from the repository's VCS
// tag or commit, when there is one.
func resolveVersion(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != devVersion || !ok {
		return stamped
	}

	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	return stamped
}

// exitCode maps a command's outcome to the exit code contract: 0 on success,
// 1 when secrets were found, and 2 for any other error, which is printed.
func exitCode(stderr io.Writer, err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, command.ErrFindings):
		return 1
	default:
		_, _ = fmt.Fprintf(stderr, "gelato: error: %s\n", err.Error())

		return 2
	}
}
