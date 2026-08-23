// Command gittree browses the state of every git repository under a directory.
//
// This file stays thin on purpose: it only calls cli.Main, so the whole CLI
// can be exercised in-process by tests and walked by tools/gendocs.
package main

import (
	"os"

	"github.com/schretzi/gittree/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
