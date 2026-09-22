package main

import (
	"os"

	"github.com/lshfx/seeking_the_way_to_immortality/internal/cli"
)

var version = "dev"

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, version))
}
