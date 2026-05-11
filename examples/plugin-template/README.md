# Shingan plugin template

A minimal, runnable Shingan plugin. The rule
(`experimental:todo_node_marker`) flags any workflow node whose ID
begins with `TODO_` — intentionally trivial so the focus stays on the
plugin author surface (`domain.AnalysisRule` + `plugin.MustRegister`)
rather than detection logic.

## Files

- `rule.go` — the rule implementation + `init()` registration
- `rule_test.go` — standalone tests (rule logic + name-prefix invariant)

## What the SDK gives you

A plugin is one Go file that implements two things:

1. **`domain.AnalysisRule`** — `Name() string` and
   `Analyze(*WorkflowGraph) []Finding`. Same contract as the built-in
   rules, no special interface.
2. **`init()`** — calls `plugin.MustRegister(rule, plugin.Manifest{…})`
   to declare the rule's metadata (frameworks, tags, docs URL,
   default severity).

That's it. There's no separate manifest file, no `.toml` registry, no
loader API. The Go init() mechanism does the wiring.

## Plugin name convention (v0.x)

Until Shingan v1.0, plugin rule names **must** begin with
`experimental:`. The check is enforced by `plugin.Register` and
documented in `docs/plugin-sdk.md`. The prefix lets `.shingan.yaml`
authors spot non-built-in rules at a glance and signals that the API
isn't pinned yet.

## Building a custom shingan binary with this plugin

The Shingan CLI runtime is exposed as `github.com/hatyibei/shingan/cli`
so wrapper binaries can embed the official command tree by importing
that package plus their plugin(s). The canonical wrapper lives at
`cmd/shingan-with-plugins/` in this directory:

```bash
go build -o ./shingan-with-plugins ./examples/plugin-template/cmd/shingan-with-plugins

# The plugin appears in the rule catalog:
./shingan-with-plugins rules
# experimental:todo_node_marker  warning  all  External plugin rule

# And participates in analyze alongside built-ins:
./shingan-with-plugins analyze --input=mygraph.json --output=markdown
# | experimental:todo_node_marker | TODO_classify | 100% | … |
```

Plugin authors copy `cmd/shingan-with-plugins/` into their own repo
and replace the `_ "github.com/hatyibei/shingan/examples/plugin-template"`
import with their plugin package's import path. The rest of the
wrapper (calling `cli.Run(os.Args[1:])`) stays identical.

The integration tests for this template
(`cli/plugin_integration_test.go`) also demonstrate the wiring — they
side-effect-import this package inside the test binary and assert
the rule appears in `shingan rules` output and
`shingan rules --format=json`.

## Running the example via tests

```bash
go test ./examples/plugin-template/...
# Runs the plugin's own tests (no shingan binary needed).

go test ./cmd/shingan/ -run TestPlugin
# Runs the integration tests that side-effect-import this package
# and assert it appears in the catalog.
```

## Running the example via shingan binary (after v0.9.0)

When the CLI extraction lands, the canonical wrapper looks like:

```go
// cmd/shingan-with-my-plugins/main.go
package main

import (
    "os"

    _ "github.com/your-org/your-plugin-repo"  // ← side-effect import
    "github.com/hatyibei/shingan/cli"
)

func main() {
    os.Exit(cli.Run(os.Args[1:]))
}
```

```bash
go build -o ./shingan ./cmd/shingan-with-my-plugins
./shingan analyze --format=langgraph --input=path/to/graph.py
./shingan rules --format=json | jq '.[] | select(.stability == "experimental")'
```

## Roadmap pointer

Full plugin SDK roadmap: [`docs/plugin-sdk.md`](../../docs/plugin-sdk.md).

Stability commitment: [`README.md` § Stability commitment](../../README.md#stability-commitment).
