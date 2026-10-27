// Package command implements gelato's subcommands as kong command structs.
package command

import (
	"errors"
	"io"
	"os"

	"golang.org/x/term"
)

// Globals holds the flags shared by every command.
type Globals struct {
	Config  []string `help:"Read configuration from this file. Repeatable; list values are concatenated in order." name:"config" sep:"none" type:"path"`
	NoColor bool     `help:"Disable colored output."                                                                 name:"no-color"`
}

// ErrFindings is returned by a command that ran successfully but found
// secrets. It is the only error that maps to exit code 1.
var ErrFindings = errors.New("secrets found")

// useColor reports whether output to w is colored: only when it is a
// terminal, as tartufo decides, and neither --no-color nor a non-empty
// NO_COLOR environment variable (https://no-color.org) turns it off.
func useColor(g *Globals, w io.Writer) bool {
	if g.NoColor || os.Getenv("NO_COLOR") != "" {
		return false
	}

	f, ok := w.(*os.File)

	return ok && term.IsTerminal(int(f.Fd()))
}
