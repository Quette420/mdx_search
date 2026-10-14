// Package cli implements the mdx command-line interface.
package cli

import (
	"fmt"
	"io"
	"os"
)

// Version is set at build time via -ldflags "-X .../internal/cli.Version=...".
var Version = "dev"

const usageText = `mdx — fast local search and an overview map for a folder of Markdown notes.

Usage:
  mdx index  [-root .] [-db path] [-ext .md,.markdown] [-exclude pattern]...
  mdx search [-k 10] [-f key=value]... [-path sub] [-since d] [-show fields] [-full] [-json] "query"
  mdx digest [-out INDEX.md] [-group field] [-show fields] [-since d] [-summarize] [-model m] [-limit n]
  mdx stats  [-field name]...
  mdx version

Every command accepts -root (corpus directory, default ".") and -db (index
path, default <root>/.mdx/index.gob). -since accepts 2026-01-15, 7d, 3w or 2m.
Run "mdx <command> -h" for all flags of a command.
`

// Run executes the CLI and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	var err error
	switch args[0] {
	case "index", "i":
		err = cmdIndex(args[1:], stdout)
	case "search", "s":
		err = cmdSearch(args[1:], stdout)
	case "digest", "d":
		err = cmdDigest(args[1:], stdout, stderr)
	case "stats":
		err = cmdStats(args[1:], stdout)
	case "version", "-v", "--version":
		fmt.Fprintln(stdout, "mdx", Version)
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usageText)
	default:
		fmt.Fprintf(stderr, "mdx: unknown command %q\n\n%s", args[0], usageText)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "mdx:", err)
		return 1
	}
	return 0
}

// Main is the entry point used by cmd/mdx.
func Main() { os.Exit(Run(os.Args[1:], os.Stdout, os.Stderr)) }
