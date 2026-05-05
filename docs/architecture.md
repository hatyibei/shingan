> 🌐 Language: **English** | [日本語](./architecture.ja.md)

# Shingan Architecture Details

```
Created:        2026-04-14
Target version: v0.1
```

---

## 1. Layer structure and dependency direction

Shingan adopts the Onion Architecture. Dependencies always flow from outer layers to inner layers — reverse dependencies are forbidden.

```
┌──────────────────────────────────────────────────────────────────┐
│  cmd/                                                            │
│    shingan/main.go  — cobra command definitions, Factory calls,  │
│                       DI wiring                                  │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │  infrastructure/                                           │  │
│  │    parser/      — JSON / ADK-Go parser implementations     │  │
│  │    reporter/    — Text / Markdown / JSON reporter impls    │  │
│  │    factory/     — AnalyzerFactory / ParserFactory impls    │  │
│  │  ┌──────────────────────────────────────────────────────┐  │  │
│  │  │  application/                                        │  │  │
│  │  │    orchestrator.go  — AnalysisOrchestrator           │  │  │
│  │  │    interfaces.go    — WorkflowParser/ReportFormatter │  │  │
│  │  │  ┌────────────────────────────────────────────────┐  │  │  │
│  │  │  │  domain/                                       │  │  │  │
│  │  │  │    model/    — WorkflowGraph / Node / Edge     │  │  │  │
│  │  │  │    rule/     — AnalysisRule interface, Finding │  │  │  │
│  │  │  │    analyzer/ — cycle / unreachable / error     │  │  │  │
│  │  │  │              — cost / redundant rule impls     │  │  │  │
│  │  │  │    testutil/ — builder.go (test graph builder) │  │  │  │
│  │  │  └────────────────────────────────────────────────┘  │  │  │
│  │  └──────────────────────────────────────────────────────┘  │  │
│  └────────────────────────────────────────────────────────────┘  │
└──────────────────────────────────────────────────────────────────┘
```

### Dependency rules (strict)

| Layer | Allowed imports | Forbidden imports |
|---|---|---|
| domain/ | standard library only | application/, infrastructure/, cmd/ |
| application/ | domain/ | infrastructure/, cmd/ |
| infrastructure/ | application/, domain/ | cmd/ |
| cmd/ | infrastructure/, application/, domain/ | — |

---

## 2. Responsibilities of each layer

### domain/

- `model.WorkflowGraph` — graph representation of nodes and edges
- `model.Node` — node type (LLM / Tool / Loop / Branch, etc.) and metadata
- `model.Edge` — directed edge with conditional label
- `rule.AnalysisRule` — analysis rule interface (`Analyze(graph) []Finding`)
- `rule.Finding` — detection result (RuleID, Severity, Message, NodeID)
- `rule.Severity` — enum of Info / Warning / Critical
- `analyzer/` — five analysis rule implementations (no external dependencies, purely functional)

The domain layer pulls in zero external libraries. This means unit tests can be written without mocks.

### application/

- `WorkflowParser` interface — `Parse(input) (*WorkflowGraph, error)`
- `ReportFormatter` interface — `Format(findings) string`
- `AnalysisOrchestrator` — runs rules concurrently with goroutines and aggregates results

Interfaces are defined on the **consumer side** (application/), not the implementation side (infrastructure/). This is the principle of Dependency Inversion.

### infrastructure/

- `parser/json` — deserializer for Shingan's own JSON schema
- `parser/adkgo` — Go AST analysis (`go/parser` / `go/ast`) to extract agent definitions
- `reporter/text` / `reporter/markdown` / `reporter/json` — output format implementations
- `factory/` — concrete implementations of AnalyzerFactory / ParserFactory / ReporterFactory

### cmd/

- cobra command definitions (`analyze` subcommand)
- Calls Factories to inject dependencies
- Determines the exit code (highest Severity → 0/1/2)

---

## 3. Factory Pattern details

### AnalyzerFactory

```
AnalyzerFactory
  └── Build(rules []string) []domain.AnalysisRule
        ├── "cycle_detection"      → CycleDetector{}
        ├── "unreachable_node"     → UnreachableNodeDetector{}
        ├── "error_handler_checker"→ ErrorHandlerChecker{}
        ├── "cost_estimation"      → CostEstimator{}
        └── "redundant_llm_call"   → RedundantLLMDetector{}
```

To add a new rule, simply add a file to `domain/analyzer/` and register a single line in the AnalyzerFactory map.

### ParserFactory

```
ParserFactory
  └── Build(format string) application.WorkflowParser
        ├── "json"   → JSONParser{}
        └── "adk-go" → ADKGoParser{}
```

To add a new format, add an implementation under `infrastructure/parser/` and register it in ParserFactory.

### ReporterFactory

```
ReporterFactory
  └── Build(output string) application.ReportFormatter
        ├── "text"     → TextReporter{}
        ├── "markdown" → MarkdownReporter{}
        └── "json"     → JSONReporter{}
```

---

## 4. Concurrency design

`AnalysisOrchestrator.Run()` executes all analysis rules in parallel using goroutines.

```
Run(graph *WorkflowGraph, rules []AnalysisRule) []Finding
  │
  ├── goroutine: rules[0].Analyze(graph) → ch
  ├── goroutine: rules[1].Analyze(graph) → ch
  ├── goroutine: rules[2].Analyze(graph) → ch
  ├── goroutine: rules[3].Analyze(graph) → ch
  └── goroutine: rules[4].Analyze(graph) → ch
                  ↓
          wait for completion via sync.WaitGroup
                  ↓
          aggregate and return []Finding
```

**Design assumptions:**
- `graph` is read-only (Analyze does not mutate the graph)
- Each goroutine writes Findings to its own independent slice → results are aggregated via channel
- No data races (`go test -race` stays green)

---

## 5. Extension points

### Adding a new analysis rule

1. Create `<rule_name>.go` under `domain/analyzer/` and implement the `AnalysisRule` interface
2. Create `domain/analyzer/<rule_name>_test.go` (build graphs with testutil/builder.go)
3. Add a single line to the map in `infrastructure/factory/analyzer_factory.go`
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
