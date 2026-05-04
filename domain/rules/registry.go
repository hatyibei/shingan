package rules

import "github.com/hatyibei/shingan/domain"

// builtinRegistry holds the set of rules that ship with Shingan. It is
// populated lazily by init() functions in each rule file (registerBuiltin).
//
// External callers MUST go through AllBuiltins(); the registerBuiltin function
// is intentionally unexported per ADR-010 (Plugin SDK is internal-only until
// v1.0). Forking and adding a rule via init() inside the rules package is the
// supported v0.x extension path.
var builtinRegistry []domain.AnalysisRule

// registerBuiltin adds rule to the builtin registry. It is intended to be
// called from init() blocks inside this package.
//
// The rule must implement domain.AnalysisRule so that legacy callers (CLI,
// MCP server, web service, third-party tests) keep working. Refactored rules
// implement BOTH AnalysisRule and the appropriate tier interface
// (LocalRule / PathRule / GlobalRule); the orchestrator type-asserts to the
// tier interface for the 1-walk dispatch path.
func registerBuiltin(r domain.AnalysisRule) {
	builtinRegistry = append(builtinRegistry, r)
}

// AllBuiltins returns a fresh slice containing every rule registered via
// registerBuiltin. The application layer (factory) calls this to obtain the
// default rule set.
//
// A copy is returned so callers cannot mutate the internal registry.
func AllBuiltins() []domain.AnalysisRule {
	out := make([]domain.AnalysisRule, len(builtinRegistry))
	copy(out, builtinRegistry)
	return out
}
