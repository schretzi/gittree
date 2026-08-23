package main

import (
	"flag"
	"fmt"
	"os"
)

// newFlagSet defines gendocs' own flags. It is a plain stdlib flag.FlagSet:
// this is a build tool, not part of the shipped CLI, so it has no business
// pulling cobra into its own interface.
func newFlagSet(mdDir, manDir, compDir *string) *flag.FlagSet {
	fs := flag.NewFlagSet("gendocs", flag.ContinueOnError)
	fs.StringVar(mdDir, "md", "", "directory to write markdown docs into")
	fs.StringVar(manDir, "man", "", "directory to write man pages into")
	fs.StringVar(compDir, "completions", "", "directory to write shell completion scripts into")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: gendocs [-md dir] [-man dir] [-completions dir]")
		fs.PrintDefaults()
	}
	return fs
}
