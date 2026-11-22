// Command halyardctl is the command line for the mail client: send, search, list, read and everything else the window
// does, from a terminal or a script.
package main

import (
	"os"

	"github.com/patrickryan01/halyard/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr)) }
