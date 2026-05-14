# Plugin SDK (v0.9 public → v1.0 GA)

ADR-010 in `shingan-adr.md` recorded the original "Plugin SDK stays
internal-only until v1.0" decision; the v0.9 implementation supersedes it
with a prefix-gated early-access path (see the ADR-010 status note). This
page is the operational state of the rollout — what ships today, what's
planned, and what stability promise covers each surface.

## Why a Plugin SDK

Linters that win their categories all share one property: they let
*users* author rules without forking the analyzer. ESLint has ~400
plugins, Ruff has 800+ rules across categories, golangci-lint has
~50 linters bundled and an external-tool runner. Shingan needs the
same trajectory so the 22 built-in rules don't become the ceiling on
what the tool can catch in production agent workflows.

## What ships today (v0.9)

**Rule catalog (in-tree, machine-readable).** Every built-in rule
publishes a `RuleManifest` — Name, Severity, Fixable, Description,
Frameworks, Tags, Stability, DocsURL. Two surfaces:

```bash
# Terminal table
shingan rules

# Machine-readable JSON catalog
shingan rules --format=json
```

The JSON form is intended to be consumed by:

- IDE / LSP rule-hover providers (so hovering over a finding shows the
  full manifest, not just the rule name)
- `shingan.dev` catalog page (planned v0.9)
- SARIF taxonomy generator (each rule becomes a `reportingDescriptor`
  with Tags as `kinds`)
- CI policy generators (build a `.shingan.yaml` template per
  framework from `shingan rules --format=json | jq …`)

**Static metadata source of truth.** `application/rule_catalog.go`
holds the `staticRuleMeta` table — when a new rule is added, the same
file gets a one-line entry. The CI test
`TestListRuleManifests_StaticTableCoversAllRules` (`cmd/shingan/`)
fails the build if a registered rule is missing from the table.

**Two registries, by design.** `domain/rules/registry.go` exports
`AllBuiltins()` and keeps `registerBuiltin` unexported — built-in rules
self-register there at `init()` time. *External* rules register through
the separate public `plugin` package (below). Built-ins and plugin rules
stay in distinct registries so the catalog can always tell them apart
(stability flag, SARIF taxonomy namespace, `experimental:` prefix).

## v0.9 public surface

**Public registration API.** Package
`github.com/hatyibei/shingan/plugin` exports `Register`,
`MustRegister`, the `Manifest` type, and registry accessors. See the
package doc for the authoring contract and validation rules
(experimental: prefix mandatory through v0.x, at least one framework
+ tag, no collisions with built-ins).

**Working example ✓** `examples/plugin-template/`
ships a runnable plugin (`experimental:todo_node_marker`) plus tests
that side-effect-import it and assert it appears in the
`shingan rules` catalog. This is the reference implementation plugin
authors should mirror.

**Catalog integration ✓** `application.ListRuleManifests`
merges plugin rules into the catalog so `shingan rules` and
`shingan rules --format=json` surface them with
`"stability": "experimental"`. The same output flows to IDE rule
hovers, SARIF taxonomy emitters, and `.shingan.yaml` generators
without special-casing.

**Analyze integration ✓** `shingan analyze` appends
`plugin.Rules()` to the built-in slice so plugin findings appear in
normal analysis output and are subject to the same severity overrides
/ ignore comments / baseline suppression as built-ins.

**Plugin author helpers ✓** Two small helpers replace
the boilerplate plugin authors otherwise rewrite per rule:

```go
// Construct a Finding with sensible defaults (Confidence=1.0,
// ConfidenceReason=ReasonExactStaticMatch). Override on the returned
// struct for heuristic detections.
plugin.NewFinding(ruleName, nodeID, domain.Warning, "message")

// Iterate nodes by type without writing the for/switch:
for _, n := range plugin.NodesOfType(g, domain.NodeTypeLLM) {
    // ...
}
```

Scope is deliberately narrow — the SDK ships only helpers that 80% of
real rules will use. More helpers will land as patterns surface from
actual plugin authors (Plugin SDK is experimental until v1.0).

**SARIF taxonomy with plugin namespace ✓** SARIF
output (`shingan analyze --output=sarif`) now carries the full rule
manifest on every `reportingDescriptor`:

```json
{
  "id": "experimental:todo_node_marker",
  "helpUri": "https://example.com/plugin-docs",
  "shortDescription": { "text": "..." },
  "properties": {
    "stability": "experimental",
    "frameworks": ["langgraph"],
    "tags": [
      "shingan-rule",
      "stability:experimental",
      "category:company-convention",
      "framework:langgraph"
    ]
  }
}
```

GitHub Code Scanning renders `properties.tags` as filter chips, so
teams can scope alerts by `stability:stable` vs
`stability:experimental` to triage built-in vs plugin findings
separately. The legacy SARIF shape (no helpUri, no tags) is preserved
when no metadata is attached — backwards compatible.

**Plugin version compat check ✓** Plugins declare the
minimum shingan release they were built against:

```go
plugin.MustRegister(MyRule{}, plugin.Manifest{
    // ...
    MinShinganVersion: "0.9.0",
})
```

At `init()` time, `plugin.Register` compares the plugin's
`MinShinganVersion` against the binary's `version.Version` (set via
`-ldflags '-X github.com/hatyibei/shingan/version.Version=...'`,
populated by goreleaser). If the binary is older, registration fails
with `ErrVersionMismatch` and a message naming both versions. Dev
builds (binary version `"dev"`) accept every plugin so local
iteration isn't blocked. Plugins that genuinely don't depend on a
specific SDK feature can leave `MinShinganVersion` empty to opt out.

```
$ shingan version
0.8.0

$ shingan-with-plugins rules
panic: plugin.MustRegister: plugin: shingan binary is older than
plugin's MinShinganVersion: binary=0.8.0, plugin requires >=0.9.0
```

**`.shingan.yaml plugins:` declaration ✓** Projects
declare which plugin rules they depend on:

```yaml
# .shingan.yaml
plugins:
  - experimental:todo_node_marker
  - experimental:company_naming
```

At analyze time, shingan verifies every name in `plugins:` is present
in the running binary's catalog. If any are missing, analyze fails
with a build-pointer error so the user knows to switch to the
wrapper binary that ships those plugins. This bridges the gap between
Go's static linkage (no dynamic plugin loading) and the ESLint-style
plugin list that teams expect their project config to capture: the
YAML declares intent, the build pipeline produces a binary that
fulfils it. Empty/missing `plugins:` key opts out of the check.

**Custom binary build flow ✓** The CLI runtime is
exposed as `github.com/hatyibei/shingan/cli` with two exports:
`Run(args []string) int` and `NewRootCmd() *cobra.Command`. Plugin
wrapper binaries `_ "your-plugin"` + `cli.Run(os.Args[1:])` and they
get the full official command tree (analyze, rules, list-rules,
explain) with the plugin's rules merged in. See
`examples/plugin-template/cmd/shingan-with-plugins/main.go` for the
canonical 5-line wrapper.

**Sample external repo ⏳ (planned).**
`github.com/hatyibei/shingan-rule-template` will be a standalone
forkable repo that mirrors `examples/plugin-template/` (rule + tests +
wrapper binary + GitHub Actions workflow). The in-tree
`examples/plugin-template/` is the reference today; the standalone repo
just removes the "clone the monorepo to see the example" friction.

### v0.9 API quick reference

The public surface that's stable as of v0.9:

```go
package plugin

// Register validates and stores a plugin rule. The rule's Name()
// must begin with `plugin.ExperimentalPrefix` ("experimental:") so
// `.shingan.yaml` authors can spot non-built-in rules at a glance.
// External rules cannot share Names with built-ins.
// Returns an error on validation failure; the rule is NOT registered.
func Register(rule domain.AnalysisRule, m Manifest) error

// MustRegister is the init()-friendly wrapper around Register that
// panics on validation failure. Use this in plugin init() blocks.
func MustRegister(rule domain.AnalysisRule, m Manifest)

// Manifest is the external-author-facing rule metadata, validated at
// Register() time and surfaced in `shingan rules --format=json`.
type Manifest struct {
    Severity          domain.Severity // optional; defaults to Info
    Description       string          // optional; one-line summary (no \n / control chars). Surfaced in `shingan rules`, IDE rule-hover, SARIF shortDescription
    Fixable           bool            // optional; whether the rule can emit an autofix
    Frameworks        []string        // required; at least one of: langgraph, crewai, n8n, adk-go, samurai, json, all
    Tags              []string        // required; at least one non-empty entry
    DocsURL           string          // optional; surfaced in IDE rule hovers + SARIF helpUri (must be absolute)
    MinShinganVersion string          // optional; minimum binary semver (no leading `v`); empty opts out of the compat check
}

// RegisteredRules / Rules: catalog accessors used by the
// application-layer catalog renderer + the analyze command's rule
// gathering. Plugin authors don't call these directly.
func RegisteredRules() []Registered
func Rules() []domain.AnalysisRule
```

```go
// version package — single source of truth for the binary's release
// string. Plugin authors don't import this; they declare
// `MinShinganVersion` in their Manifest and the SDK reads
// version.Version for them.
package version

// Version is the shingan release tag this binary was built for
// (semver, no leading `v`). Defaults to "dev"; goreleaser injects
// the real value via ldflags at release time.
var Version = "dev"

// IsDev reports whether the running binary is a development build.
// MinShinganVersion checks treat dev builds as compatible with
// everything so local iteration isn't blocked.
func IsDev() bool
```

External rule template:

```go
package myrules // some-other-repo: github.com/acme/shingan-rules

import (
    "github.com/hatyibei/shingan/domain"
    "github.com/hatyibei/shingan/plugin"
)

type CompanyNaming struct{}

func (CompanyNaming) Name() string { return "experimental:company_naming" }
func (CompanyNaming) Analyze(g *domain.WorkflowGraph) []domain.Finding {
    // ...
}

func init() {
    plugin.MustRegister(CompanyNaming{}, plugin.Manifest{
        Description:       "Flags agent names that violate the company convention",
        Frameworks:        []string{"langgraph"},
        Tags:              []string{"company-convention"},
        DocsURL:           "https://acme.example/shingan-rules/company-naming",
        MinShinganVersion: "0.9.0",
    })
}
```

**Building a binary that includes the plugin.** Go's `plugin` package
isn't cross-compilable, so v0.9 uses `init()`-time static linkage, not
dynamic loading. The plugin author (or the consuming team) ships a tiny
wrapper `main` that side-effect-imports the rule package and calls
`cli.Run` — that single binary is the full official command tree with
the plugin's rules merged in:

```go
package main

import (
    "os"

    "github.com/hatyibei/shingan/cli"
    _ "github.com/acme/shingan-rules" // init() registers the rule
)

func main() { os.Exit(cli.Run(os.Args[1:])) }
```

See `examples/plugin-template/cmd/shingan-with-plugins/main.go` for the
canonical version. (A future `shingan build --with-plugin=...` helper
that generates this wrapper automatically — mirroring golangci-lint's
"custom" build flow — is on the v0.10+ roadmap, not shipped.)

**User config to consume it.** `.shingan.yaml` declares which plugin
rules the project depends on by *rule name* (the `experimental:`-prefixed
`Name()`, not a repo path); analyze fails fast if the running binary
doesn't carry them. Severity is tuned through the same `rules:` key as
built-ins:

```yaml
# .shingan.yaml
plugins:
  - experimental:company_naming   # rule name, verified against the binary's catalog

rules:
  experimental:company_naming:
    severity: warning
```

See [`docs/severity-policy.md`](./severity-policy.md) for the full
`.shingan.yaml` schema.

## What ships at v1.0

**Stability promise on the plugin ABI.** The `plugin.Manifest` struct,
the `plugin.Register` signature, the `experimental:` prefix
requirement, and the `Name()`/`Analyze()` contract from
`domain.AnalysisRule` are pinned through v2.0.

**Drop the `experimental:` prefix requirement.** External rules can
ship with arbitrary names (collision-checked at registration). Rules
that opt-in to "stable" by passing `Stability: "stable"` in their
manifest enter the same severity-override and SARIF taxonomy as
built-ins.

## Stability commitment by surface

| Surface | Stable through | Notes |
| --- | --- | --- |
| `shingan rules --format=json` schema | v1.0 | additive fields only; existing fields never renamed/typed-changed |
| `plugin.Manifest` struct | v0.9 experimental; v1.0 GA | additive fields possible before v1.0; existing fields won't be renamed |
| `plugin.Register` / `MustRegister` signature | v0.9 experimental; v1.0 GA | `experimental:` prefix mandatory until v1.0 |
| `version.Version` string format | v0.x onwards | semver, no leading `v`; part of the `MinShinganVersion` compat contract |
| `domain.AnalysisRule` interface | v2.0 | the load-bearing rule contract — promise won't move |
| `application.RuleManifest` Go struct | v1.0 (external import) | the in-tree catalog struct; plugin authors don't import it — they supply `plugin.Manifest` |
| `.shingan.yaml` `plugins:` key | v0.9 onwards | not present in v0.8 |

## Authoring a built-in rule (upstream contribution)

The plugin path above is for rules that live in *your* repo. If instead
you want a rule merged into Shingan itself as a built-in:

1. Add your rule to `domain/rules/` with an `init()` that calls
   `registerBuiltin`.
2. Add the corresponding row to `staticRuleMeta` in
   `application/rule_catalog.go`.
3. Add an explanation block to `application/explain.go`
   (`RuleExplanations` map).
4. Run `go test ./...` to confirm the catalog tests pass.
5. Open a PR.

This is identical to how the existing built-in rules are authored —
there's no hidden API. See [`docs/rule-authoring.md`](./rule-authoring.md)
for the full builtin-authoring guide (three-tier templates,
ConfidenceReason selection, TDD patterns).

## Roadmap pointer

The full roadmap (Phase A/B/C trust → distribution → value capture)
lives in `~/.claude/projects/-home-hatyibei-Claude/memory/project_shingan_trust_strategy_2026_05_09.md`.
Plugin SDK is the centerpiece of Phase B-1.
