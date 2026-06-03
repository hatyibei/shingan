> 🌐 Language: **English** | [日本語](./architecture.ja.md)

# Shingan Architecture Details

```
Created:        2026-04-14
Updated:        2026-06-03
Current version: v0.9
```

---

## 1. Layer structure and dependency direction

Shingan adopts the Onion Architecture. Dependencies always flow from outer layers to inner layers — reverse dependencies are forbidden.

```
┌──────────────────────────────────────────────────────────────────┐
│  cmd/                                                            │
│    shingan/ api/ runner/ shingan-web/ shingan-lsp/ shingan-mcp/  │
│    shingan-gen/  — cobra commands, Factory calls, DI wiring      │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  infrastructure/                                           │  │
│  │    parser/      — 11 framework parsers (JSON / ADK-Go /     │  │
│  │                   LangGraph / CrewAI / n8n / … )            │  │
│  │    reporter/    — Markdown / JSON / SARIF reporter impls    │  │
│  │    factory/     — AnalyzerFactory / ParserFactory impls     │  │
│  │    api/ baseline/ cache/  — service, baseline, cache impls  │  │
│  │  ┌──────────────────────────────────────────────────────┐  │  │
│  │  │  application/                                        │  │  │
│  │  │    orchestrator.go  — AnalysisOrchestrator           │  │  │
│  │  │    parser.go / reporter.go — consumer-side interfaces│  │  │
│  │  │    policy.go / rule_catalog.go — .shingan.yaml + catalog │
│  │  ┌────────────────────────────────────────────────┐  │  │  │
│  │  │  domain/                                       │  │  │  │
│  │  │    graph.go    — WorkflowGraph / Node / Edge   │  │  │  │
│  │  │    rule.go     — AnalysisRule + tier ifaces    │  │  │  │
│  │  │    finding.go  — Finding / Severity            │  │  │  │
│  │  │    rules/      — 22 built-in rule impls        │  │  │  │
│  │  │              (registry.go AllBuiltins())       │  │  │  │
│  │  │    testutil/ — builder.go (test graph builder) │  │  │  │
│  │  └────────────────────────────────────────────────┘  │  │  │
│  │  └──────────────────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

> The `domain` package itself holds the core types (`graph.go`, `rule.go`,
> `finding.go`, `visitor.go`, `baseline.go`); the rule **implementations** live
> in the `domain/rules` sub-package (there is no `domain/analyzer` package).

### Dependency rules (strict)

| Layer | Allowed imports | Forbidden imports |
|---|---|---|
| domain/ | standard library only | application/, infrastructure/, cmd/ |
| application/ | domain/ (+ two documented exceptions — see §7) | infrastructure/, cmd/ |
| infrastructure/ | application/, domain/ | cmd/ |
| cmd/ | infrastructure/, application/, domain/ | — |

> The "application → domain only" row is the *ideal*. The code has two
> deliberate, bounded exceptions (`gopkg.in/yaml.v3` and the `plugin` package);
> the real dependency graph and its rationale are documented in §7 and
> ADR-017.

---

## 2. Responsibilities of each layer

### domain/

- `WorkflowGraph` — graph representation of nodes and edges (`graph.go`)
- `Node` — node type (LLM / Tool / Task / Loop / Branch, etc.) and metadata
- `Edge` — directed edge with conditional label
- `AnalysisRule` — legacy analysis rule interface (`Analyze(graph) []Finding`);
  new rules implement the `LocalRule` / `PathRule` / `GlobalRule` tier
  interfaces (ADR-006/007) so the single-walk `GraphWalker` can dispatch them
- `Finding` — detection result (RuleName, Severity, Message, NodeID, Confidence)
- `Severity` — enum of Info / Warning / Critical
- `rules/` — **22 built-in rule implementations**, each self-registered via its
  own `init()` into the registry; `rules.AllBuiltins()` returns the full set

The domain layer pulls in zero external libraries (standard library only).
This means unit tests can be written without mocks.

### application/

- `WorkflowParser` interface — `Parse(input) (*WorkflowGraph, error)`
- `ReportFormatter` interface — `Format(findings) string`
- `AnalysisOrchestrator` — runs rules concurrently with goroutines and aggregates results

Interfaces are defined on the **consumer side** (application/), not the implementation side (infrastructure/). This is the principle of Dependency Inversion.

### infrastructure/

- `parser/` — **11 framework parsers** mapping each framework onto the shared
  `WorkflowGraph` IR: `json` (native schema), `adkgo` (Go AST via `go/parser`),
  `samurai`, `langgraph`, `n8n`, `crewai`, plus the five added in v0.9
  (`langgraph-js`, `mastra`, `pydantic-graph`, `llamaindex`, `autogen`). The
  Python/TS-backed parsers run their shim in a long-lived subprocess over
  JSON-RPC; n8n is pure Go.
- `reporter/markdown` / `reporter/json` / `reporter/sarif` — output formats
- `factory/` — concrete implementations of AnalyzerFactory / ParserFactory / ReporterFactory
- `api/`, `baseline/`, `cache/` — HTTP service, `--save-baseline` support, and
  the parse/analysis cache

### cmd/

- cobra command definitions (`analyze` subcommand)
- Calls Factories to inject dependencies
- Determines the exit code (highest Severity → 0/1/2)

---

## 3. Factory Pattern details

### AnalyzerFactory

The factory no longer holds a hardcoded rule map. It delegates to
`rules.AllBuiltins()`, which returns every rule that self-registered via its
own `init()` block — so adding a rule never touches the factory (ADR-010,
internal-first Plugin SDK).

```
AnalyzerFactory
  ├── Create(ruleType string) (domain.AnalysisRule, error)
  │     └── walks rules.AllBuiltins(), matches by Name()
  └── CreateAll() []domain.AnalysisRule
        └── rules.AllBuiltins()   // all 22 built-ins
```

To add a new rule, drop a file under `domain/rules/` whose `init()` registers
it — the factory and CLI pick it up automatically.

### ParserFactory

```
ParserFactory
  └── Build(format string) application.WorkflowParser
        ├── "json" "adk-go" "samurai" "langgraph" "n8n" "crewai"
        └── "langgraph-js" "pydantic-graph" "llamaindex" "autogen" "mastra"
```

To add a new format, add an implementation under `infrastructure/parser/` and register it in ParserFactory.

### ReporterFactory

```
ReporterFactory
  └── Build(output string) application.ReportFormatter
        ├── "markdown" → MarkdownReporter{}
        ├── "json"     → JSONReporter{}
        └── "sarif"    → SARIFReporter{}
```

---

## 4. Concurrency design

`AnalysisOrchestrator.Run()` executes all analysis rules in parallel using goroutines.

```
Run(graph *WorkflowGraph, rules []AnalysisRule) []Finding
  │
  ├── goroutine: rules[0].Analyze(graph) → ch
  ├── goroutine: rules[1].Analyze(graph) → ch
  ├── … one goroutine per rule (all 22 built-ins by default) …
  └── goroutine: rules[n-1].Analyze(graph) → ch
                  ↓
          wait for completion via sync.WaitGroup
                  ↓
          aggregate and return []Finding
```

> Single-node / single-path rules implemented against the tier interfaces
> (`LocalRule` / `PathRule` / `GlobalRule`) are additionally dispatched by a
> single shared `GraphWalker` pass (ADR-006/007) rather than each re-walking the
> graph; the `AnalysisRule.Analyze` shape above is the legacy/compat view.

**Design assumptions:**
- `graph` is read-only (Analyze does not mutate the graph)
- Each goroutine writes Findings to its own independent slice → results are aggregated via channel
- No data races (`go test -race` stays green)

---

## 5. Extension points

### Adding a new analysis rule

1. Create `<rule_name>.go` under `domain/rules/` implementing a tier interface
   (`LocalRule` / `PathRule` / `GlobalRule`) — or the legacy `AnalysisRule` —
   and self-register it in an `init()` block
2. Create `domain/rules/<rule_name>_test.go` (build graphs with testutil/builder.go)
3. No factory edit needed — `rules.AllBuiltins()` picks it up automatically
4. Confirm that `go test ./... && go vet ./...` is green

### Adding a new parser

1. Create `infrastructure/parser/<format>/parser.go` implementing `application.WorkflowParser`
2. Add a branch in `infrastructure/factory/parser_factory.go`
3. Add sample files under `testdata/<format>/` and write tests

### Adding a new Reporter

1. Create `infrastructure/reporter/<format>/reporter.go` implementing `application.ReportFormatter`
2. Add a branch in `infrastructure/factory/reporter_factory.go`

---

## 6. ADR index

For details on the design decisions, see `shingan-adr.md`.

| ADR | Title |
|---|---|
| ADR-001 | Product selection — why "static analysis of AI agent workflows" |
| ADR-002 | Selection of target frameworks |
| ADR-003 | Architecture design (Onion Architecture + Factory Pattern) |
| ADR-004 | Infrastructure design (parsers, reporters, CLI) |
| ADR-005 | Implementation scope and schedule |
| Appendix A | Glossary |
| Appendix B | SamuraiAI ↔ ADK-Go node mapping |
| Appendix C | Detailed analysis rule specifications |

(The table above lists the founding ADRs; later decisions — ADR-006 … ADR-017 —
live in `shingan-adr.md` directly.)

---

## 7. Documented dependency exceptions (the real graph)

The "application depends only on domain" rule in §1 is the *ideal*. The code
has two deliberate, bounded exceptions. Rather than claim a purity the code
doesn't have, the real graph is recorded here and in **ADR-017**:

```
application/policy.go        → gopkg.in/yaml.v3          (.shingan.yaml parsing)
application/rule_catalog.go  → plugin                    (rule-catalog rendering)

plugin/plugin.go             → domain, domain/rules, version, golang.org/x/mod/semver
```

- **`application → gopkg.in/yaml.v3`** — `.shingan.yaml` parsing is
  policy-domain logic; the `Policy` struct lives alongside `ApplyPolicy` /
  `VerifyRequiredPlugins`, and only `LoadPolicy` touches YAML (one caller in
  `cli/analyze.go`). Splitting it into infrastructure buys nothing.
- **`application → plugin`** — `rule_catalog` must read the live plugin
  registry to render *all* rules (built-in + plugin-registered). `plugin` is the
  public SDK package, so inverting this edge is an SDK-contract change.

A faithful refactor would ripple into the public `plugin` SDK surface, so it was
deliberately **not** done (see ADR-017 for the trigger conditions that would
justify revisiting it).
