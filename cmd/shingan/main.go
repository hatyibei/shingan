// Package main is the entry point for the official `shingan` binary.
// All command logic lives in `github.com/hatyibei/shingan/cli` so
// plugin wrapper binaries can embed the same command tree by
// importing that package and calling `cli.Run`. See
// `examples/plugin-template/cmd/shingan-with-plugins/` for the
// wrapper pattern.
package main

import (
	"os"

	"github.com/hatyibei/shingan/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
