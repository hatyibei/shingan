# Plugin SDK roadmap (v0.x foundation → v1.0 GA)

ADR-015 in `shingan-adr.md` is the strategic document; this page is the
operational state of the rollout — what ships today, what's planned, and
what stability promise covers each surface.

## Why a Plugin SDK

Linters that win their categories all share one property: they let
*users* author rules without forking the analyzer. ESLint has ~400
plugins, Ruff has 800+ rules across categories, golangci-lint has
~50 linters bundled and an external-tool runner. Shingan needs the
same trajectory so the 22 built-in rules don't become the ceiling on
what the tool can catch in production agent workflows.

## What ships today (v0.8.7)

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

**Internal-only registry.** `domain/rules/registry.go` exports
`AllBuiltins()` but keeps `registerBuiltin` unexported. External
packages cannot register their own rules yet — that's deliberate per
ADR-015 ("v0.x 期間中は `experimental:` prefix 必須、no stability
promise").

## What ships at v0.9 (planned)

**Public registration API.** A new package `github.com/hatyibei/shingan/plugin`
will export:

```go
package plugin

// Register adds an external rule to the catalog with experimental
// stability. The rule's Name() must begin with "experimental:" so
// shingan.yaml authors can spot non-built-in rules at a glance.
// External rules cannot share Names with built-ins (panics at init).
func Register(rule domain.AnalysisRule, m Manifest) error

// Manifest is the external-author-facing equivalent of the internal
// RuleManifest. Required fields are validated at Register() time.
type Manifest struct {
    Frameworks []string // at least one
    Tags       []string // at least one
    DocsURL    string   // optional; surfaced in IDE hovers
}
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
    plugin.Register(CompanyNaming{}, plugin.Manifest{
        Frameworks: []string{"langgraph"},
        Tags:       []string{"company-convention"},
        DocsURL:    "https://acme.example/shingan-rules/company-naming",
    })
}
```

User config to consume it:

```yaml
# .shingan.yaml
plugins:
  - github.com/acme/shingan-rules

severity_overrides:
  experimental:company_naming: warning
```

Shingan will need to be rebuilt with the plugin imported — Go's
`plugin` package isn't cross-compilable, so v0.9 uses `init()`-time
static linkage rather than dynamic loading. A wrapper command
`shingan build --with-plugin=github.com/acme/shingan-rules` writes a
small main module, runs `go build`, and outputs a custom `shingan`
binary. Mirrors golangci-lint's "custom" build flow.

**Sample external rule repo.** `github.com/hatyibei/shingan-rule-template`
ships with a single rule, GitHub Actions workflow, and the manifest
metadata so plugin authors have a starting point.

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
| `RuleManifest` Go struct | v1.0 internal; v1.0 public | external import deferred to v0.9 |
| `domain.AnalysisRule` interface | v2.0 | the load-bearing rule contract — promise won't move |
| `plugin.Register` signature | v0.9 experimental; v1.0 GA | `experimental:` prefix mandatory until v1.0 |
| `.shingan.yaml` `plugins:` key | v0.9 onwards | not present in v0.8 |

## Pre-v0.9 escape hatches (today)

If you need a custom rule before v0.9 ships, the supported path is:

1. Fork `github.com/hatyibei/shingan`.
2. Add your rule to `domain/rules/` with an `init()` that calls
   `registerBuiltin`.
3. Add the corresponding row to `staticRuleMeta` in
   `application/rule_catalog.go`.
4. Add an explanation block to `application/explain.go`
   (`RuleExplanations` map).
5. Run `go test ./...` to confirm the catalog tests pass.
6. Build a custom binary from your fork.

This is identical to how the built-in rules are authored — there's no
hidden API. The only thing v0.9 changes is dropping the fork
requirement: rules will live in *your* repo, registered at `init()`
time by importing the `plugin` package.

## Roadmap pointer

The full roadmap (Phase A/B/C trust → distribution → value capture)
lives in `~/.claude/projects/-home-hatyibei-Claude/memory/project_shingan_trust_strategy_2026_05_09.md`.
Plugin SDK is the centerpiece of Phase B-1.
