// Package plugin is the public API for authoring third-party Shingan
// rules. External rule authors `import _ "github.com/hatyibei/shingan/plugin"`
// inside a custom wrapper binary, register their rule via Register()
// at init() time, then build a shingan binary that statically links
// the plugin.
//
// This is the v0.9 surface of ADR-015 Plugin SDK. Until v1.0 the API
// is marked experimental: external rule Names MUST start with
// "experimental:" so .shingan.yaml authors can spot non-built-in
// rules at a glance, and the Register() signature may shift between
// minor versions. v1.0 will pin both per the README stability
// commitment.
//
// Authoring a plugin:
//
//	package myrules
//
//	import (
//	    "github.com/hatyibei/shingan/domain"
//	    "github.com/hatyibei/shingan/plugin"
//	)
//
//	type MyRule struct{}
//	func (MyRule) Name() string                                    { return "experimental:my_rule" }
//	func (MyRule) Analyze(g *domain.WorkflowGraph) []domain.Finding { ... }
//
//	func init() {
//	    plugin.MustRegister(MyRule{}, plugin.Manifest{
//	        Frameworks: []string{"langgraph"},
//	        Tags:       []string{"company-convention"},
//	    })
//	}
//
// See examples/plugin-template/ in the shingan repository for a
// complete, build-and-run-able sample including the wrapper main.go.
package plugin

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
)

// ExperimentalPrefix is the mandatory Name() prefix for plugin rules
// until v1.0. Rules whose Name() does not start with this prefix are
// rejected by Register so .shingan.yaml authors can visually
// distinguish built-in rules from plugin rules.
const ExperimentalPrefix = "experimental:"

// Manifest is the author-supplied metadata for a plugin rule. Combined
// with the runtime data from the rule's domain.AnalysisRule methods
// (Name, Severity via the optional metaProvider duck-type), it
// produces the same RuleManifest that built-in rules expose to the
// catalog.
//
// Required fields are validated at Register() time:
//   - Frameworks: at least one entry, each a known framework slug
//     ("langgraph", "crewai", "n8n", "adk-go", "samurai", "json", "all").
//   - Tags: at least one entry.
//
// Optional fields:
//   - Severity: defaults to domain.Info when zero-valued.
//   - DocsURL: surfaced in IDE rule-hover providers.
type Manifest struct {
	Severity   domain.Severity
	Frameworks []string
	Tags       []string
	DocsURL    string
}

// Registered describes one plugin rule's runtime + author metadata.
// Consumers (application/rule_catalog.go, the CLI, MCP server) read
// this slice to surface plugin rules alongside built-ins.
type Registered struct {
	Rule     domain.AnalysisRule
	Manifest Manifest
}

// validFrameworks is the closed set of framework slugs Register
// accepts. Mirrors the values used in application/rule_catalog.go
// staticRuleMeta — keep in sync (the drift test in cmd/shingan/
// rules_test.go covers the built-in side; this validator covers
// plugins).
var validFrameworks = map[string]struct{}{
	"langgraph": {}, "crewai": {}, "n8n": {}, "adk-go": {},
	"samurai": {}, "json": {}, "all": {},
}

var (
	mu       sync.RWMutex
	registry []Registered
	names    = map[string]struct{}{} // de-duplication index
)

// ErrInvalidPrefix means the rule's Name() does not start with the
// experimental: prefix mandatory through v0.x.
var ErrInvalidPrefix = errors.New("plugin: rule name must start with " + ExperimentalPrefix)

// ErrEmpty means a required Manifest field (Frameworks, Tags) is empty.
var ErrEmpty = errors.New("plugin: Manifest requires non-empty Frameworks and Tags")

// ErrCollision means another rule (built-in or plugin) already
// registered the same name.
var ErrCollision = errors.New("plugin: rule name already registered")

// Register validates and stores a plugin rule. Typical use is in an
// init() function of the plugin package.
//
// Returns an error if validation fails; the rule is NOT registered in
// that case. For init()-time use see MustRegister.
func Register(rule domain.AnalysisRule, m Manifest) error {
	if rule == nil {
		return errors.New("plugin: nil rule")
	}
	name := rule.Name()
	if !strings.HasPrefix(name, ExperimentalPrefix) {
		return fmt.Errorf("%w (got %q)", ErrInvalidPrefix, name)
	}
	if len(m.Frameworks) == 0 || len(m.Tags) == 0 {
		return ErrEmpty
	}
	for _, fw := range m.Frameworks {
		if _, ok := validFrameworks[fw]; !ok {
			return fmt.Errorf("plugin: unknown framework %q (valid: langgraph, crewai, n8n, adk-go, samurai, json, all)", fw)
		}
	}
	// Collision with a built-in?
	for _, b := range rules.AllBuiltins() {
		if b.Name() == name {
			return fmt.Errorf("%w (built-in %q)", ErrCollision, name)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if _, dup := names[name]; dup {
		return fmt.Errorf("%w (plugin %q)", ErrCollision, name)
	}
	names[name] = struct{}{}
	registry = append(registry, Registered{Rule: rule, Manifest: m})
	return nil
}

// MustRegister is the init()-friendly wrapper around Register that
// panics on validation failure. Plugin authors call this at package
// init() time when an invalid registration is a build-time bug, not a
// recoverable error.
func MustRegister(rule domain.AnalysisRule, m Manifest) {
	if err := Register(rule, m); err != nil {
		panic("plugin.MustRegister: " + err.Error())
	}
}

// Registered returns a copy of the currently registered plugin rules
// in registration order. Returned slice is safe to mutate.
func RegisteredRules() []Registered {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Registered, len(registry))
	copy(out, registry)
	return out
}

// Rules returns the AnalysisRule slice for all registered plugins,
// suitable for handing to the orchestrator alongside built-ins.
func Rules() []domain.AnalysisRule {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]domain.AnalysisRule, len(registry))
	for i, r := range registry {
		out[i] = r.Rule
	}
	return out
}

// resetForTest clears the registry. Only intended for unit tests in
// this package — the public API has no use case for unregistering.
func resetForTest() {
	mu.Lock()
	defer mu.Unlock()
	registry = nil
	names = map[string]struct{}{}
}
