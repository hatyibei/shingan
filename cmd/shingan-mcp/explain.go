package main

import "github.com/hatyibei/shingan/application"

// ruleExplanations preserves the local symbol name used elsewhere in
// this package (`tools.go`, `main_test.go`) — the map itself moved to
// `application/explain.go` in v0.8.5 so the CLI's new `explain`
// subcommand and a future LSP hover handler share a single source of
// truth.
var ruleExplanations = application.RuleExplanations

// knownRuleNames mirrors application.KnownRuleNames for the same
// backward-compat reason.
func knownRuleNames() []string { return application.KnownRuleNames() }
