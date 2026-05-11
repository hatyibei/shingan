// Command shingan-with-plugins is the canonical wrapper that bundles
// the example plugin (`examples/plugin-template/`) into a custom
// shingan analyzer binary.
//
// To build:
//
//	go build -o ./shingan-with-plugins ./examples/plugin-template/cmd/shingan-with-plugins
//
// To run:
//
//	./shingan-with-plugins rules
//	# experimental:todo_node_marker appears in the listing.
//
//	./shingan-with-plugins analyze --format=json --input=mygraph.json
//	# The plugin participates in analysis alongside built-ins.
//
// Plugin authors copy this directory into their own repo, replace the
// side-effect import with their plugin package, and run `go build`.
package main

import (
	"os"

	// Side-effect import: every plugin package contributes its
	// rule(s) via init(). Add more plugins by appending additional
	// `_ "..."` imports here.
	_ "github.com/hatyibei/shingan/examples/plugin-template"

	"github.com/hatyibei/shingan/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}
