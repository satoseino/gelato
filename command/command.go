// Package command implements gelato's subcommands as kong command structs.
package command

import "errors"

// Globals holds the flags shared by every command.
type Globals struct {
	Config []string `help:"Read configuration from this file. Repeatable; list values are concatenated in order." name:"config" sep:"none" type:"path"`
}

// ErrFindings is returned by a command that ran successfully but found
// secrets. It is the only error that maps to exit code 1.
var ErrFindings = errors.New("secrets found")
