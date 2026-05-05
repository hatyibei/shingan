> 🌐 Language: **English** | [日本語](./CONTRIBUTING.ja.md)

# Contributing to Shingan

Shingan is an AI agent workflow static analyzer. Contributions are welcome — new rules, parser extensions, performance improvements, you name it.

## Development environment

- Go 1.25+
- `make`
- (optional) GCP project with Application Default Credentials for Vertex AI / Gemini smoke tests

```bash
git clone https://github.com/hatyibei/shingan.git
cd shingan
go mod tidy
go test -race ./...
go build -o shingan ./cmd/shingan
```

## Architectural principles

**Onion Architecture** — strictly enforced. Only this dependency direction is allowed:

```
cmd/  → infrastructure/  → application/  → domain/
```

- `domain/` has **zero external dependencies** (standard library only)
- `application/` may only import `domain/`
- `infrastructure/` provides concrete implementations of interfaces defined in `application/`
- Any reversed dependency is rejected on sight

See [docs/architecture.md](./docs/architecture.md) for the deep dive.

## Adding a new rule

The full procedure (tier classification, ConfidenceReason selection, listener templates, test patterns) lives in [docs/rule-authoring.md](./docs/rule-authoring.md). Short version:

1. Confirm the rule isn't already in the README's rule table
2. Open an issue (`enhancement`, `new-rule` labels)
3. Implement `LocalRule` / `PathRule` / `GlobalRule` in `domain/rules/<rule_id>.go` (ADR-007)
4. Add at least 5 test cases (positive / negative / edge / Reason stamp / Meta) in `domain/rules/<rule_id>_test.go`
5. Call `registerBuiltin(NewYourRule())` from `init()` — no factory edit required
6. Add a generator in `domain/testutil/generate.go` and a pattern in `cmd/shingan-gen/main.go`
7. Add `docs/rules/<rule-id>.md` and update the README rule table
8. Add an entry in `cmd/shingan-mcp/explain.go` (keep parity)
9. `make lint && go test -race ./...` must be green

## Before opening a PR

```bash
go vet ./...
go test -race ./...
go test -race -tags=e2e ./...   # CLI / API / Runner E2E
go build ./cmd/...               # all binaries build
```

## Commit messages

Conventional Commits preferred:
- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation only
- `test:` test additions
- `refactor:` behavior-preserving refactor
- `ci:` CI / build changes

## License

Contributions are provided under the [MIT License](./LICENSE).

## Code of conduct

Mutual respect and constructive discussion. Direct technical critique is welcome; personal attacks are not.
