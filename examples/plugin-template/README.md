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

The Shingan CLI entry point will be extractable into an importable
package in the v0.9.0 final release (tracked as a follow-up to the
v0.9 Plugin SDK landing). Until then, the supported integration path
is:

1. **Fork-and-import**: clone shingan, add a side-effect import of
   your plugin package in `cmd/shingan/main.go`'s package, rebuild.
2. **In-tree development**: drop your plugin under
   `examples/<my-plugin>/`, add a side-effect import in a test file
   to verify it loads, then publish from your own fork.

The integration tests for this template (`cmd/shingan/plugin_integration_test.go`)
demonstrate path 2 — they side-effect-import this package and assert
the rule appears in `shingan rules` and `shingan rules --format=json`
output.

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
