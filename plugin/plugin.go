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
	"regexp"
	"strings"
	"sync"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/version"
	"golang.org/x/mod/semver"
)

// validNameSuffix is the grammar plugin rule names must satisfy AFTER
// the `experimental:` prefix. Lowercase ASCII letters/digits/underscores
// only, must start with a letter, length-bounded to avoid pathological
// inputs. This is intentionally restrictive at v0.x — Codex Slice A
// flagged that the previous prefix-only check let through control
// chars, whitespace, zero-width Unicode, path-traversal-like
// characters, and case variants. Loosening later is non-breaking;
// tightening later would break authors.
var validNameSuffix = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ExperimentalPrefix is the mandatory Name() prefix for plugin rules
// until v1.0. Rules whose Name() does not start with this prefix are
// rejected by Register so .shingan.yaml authors can visually
// distinguish built-in rules from plugin rules.
const ExperimentalPrefix = "experimental:"

// Manifest is the author-supplied metadata for a plugin rule. Combined
// with the runtime data from the rule's domain.AnalysisRule methods
// (Name), it produces the same RuleManifest that built-in rules
// expose to the catalog.
//
// Required fields are validated at Register() time:
//   - Frameworks: at least one entry, each a known framework slug
//     ("langgraph", "crewai", "n8n", "adk-go", "samurai", "json", "all").
//   - Tags: at least one non-empty entry.
//
// Optional fields:
//   - Severity: defaults to domain.Info when zero-valued.
//   - Description: one-sentence summary surfaced in `shingan rules`,
//     IDE rule-hover, and SARIF reportingDescriptor.shortDescription.
//     If empty, the catalog falls back to "External plugin rule".
//   - Fixable: signals the rule emits findings with TextEdit
//     auto-fixes (used by LSP code-action providers).
//   - DocsURL: surfaced as SARIF helpUri and in IDE rule-hover.
//   - MinShinganVersion: minimum semver (no leading `v`) of the
//     shingan binary required to load this plugin. Empty means "no
//     opinion / accept any version" — only use this for plugins that
//     genuinely depend on no specific SDK feature. Recommended: pin
//     the version your plugin was built against (e.g. "0.9.0") so
//     future binary upgrades that break the plugin contract surface
//     as a clear error at Register time rather than a runtime
//     surprise.
type Manifest struct {
	Severity          domain.Severity
	Description       string
	Fixable           bool
	Frameworks        []string
	Tags              []string
	DocsURL           string
	MinShinganVersion string
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

// ErrVersionMismatch means the running shingan binary's version is
// older than the plugin's declared MinShinganVersion. Plugin authors
// can detect this case with errors.Is.
var ErrVersionMismatch = errors.New("plugin: shingan binary is older than plugin's MinShinganVersion")

// ErrBadVersion means the plugin's MinShinganVersion isn't a valid
// semver string (must be "MAJOR.MINOR.PATCH" without leading `v`).
var ErrBadVersion = errors.New("plugin: MinShinganVersion is not valid semver")

// checkShinganVersion compares the binary's `version.Version` against
// the plugin's declared MinShinganVersion. Returns nil when:
//   - the plugin opts out by leaving MinShinganVersion empty;
//   - the binary is a dev build (version.Version == "dev"), so local
//     development isn't blocked by ldflags injection state;
//   - the binary version satisfies `binary >= MinShinganVersion`.
//
// Returns ErrBadVersion when the plugin's declared MinShinganVersion
// can't be parsed, ErrVersionMismatch when the binary is too old.
func checkShinganVersion(min string) error {
	if min == "" {
		return nil
	}
	if version.IsDev() {
		return nil
	}
	wantV := "v" + min
	if !semver.IsValid(wantV) {
		return fmt.Errorf("%w: %q", ErrBadVersion, min)
	}
	// Normalise the binary version: accept "v0.9.0" too, even though
	// the canonical form is unprefixed (Codex Slice A #6 — common
	// author mistake). After this strip-then-prepend dance, gotV is
	// always semver-valid `v<x.y.z>` form for tagged releases.
	binary := strings.TrimPrefix(version.Version, "v")
	gotV := "v" + binary
	if !semver.IsValid(gotV) {
		// Binary version isn't a valid semver tag (e.g. "main", a
		// git SHA, or a malformed ldflag). Pre-fix this silently
		// passed every check; Codex Slice A #5 flagged that as a
		// production-risk bypass. Surface it as ErrBadVersion so
		// the binary's CI catches the bad build.
		return fmt.Errorf("%w (binary): %q is not valid semver; goreleaser injects MAJOR.MINOR.PATCH via -X github.com/hatyibei/shingan/version.Version=...", ErrBadVersion, version.Version)
	}
	if semver.Compare(gotV, wantV) < 0 {
		return fmt.Errorf("%w: binary=%s, plugin requires >=%s", ErrVersionMismatch, binary, min)
	}
	return nil
}

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
	// Strict suffix grammar — Codex Slice A flagged that a bare
	// HasPrefix accepts whitespace, control chars, zero-width Unicode,
	// case variants, path separators, etc. Validate the part after
	// the prefix as a slug.
	suffix := strings.TrimPrefix(name, ExperimentalPrefix)
	if !validNameSuffix.MatchString(suffix) {
		return fmt.Errorf(
			"plugin: rule name %q has invalid suffix %q — must match %s",
			name, suffix, validNameSuffix.String(),
		)
	}
	if len(m.Frameworks) == 0 || len(m.Tags) == 0 {
		return fmt.Errorf("%w (rule %q)", ErrEmpty, name)
	}
	for _, fw := range m.Frameworks {
		if _, ok := validFrameworks[fw]; !ok {
			return fmt.Errorf("plugin: rule %q: unknown framework %q (valid: langgraph, crewai, n8n, adk-go, samurai, json, all)", name, fw)
		}
	}
	// Per-entry Tags validation: reject empty, whitespace-only, or
	// control-character tags so the catalog can't render blanks.
	for _, t := range m.Tags {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("plugin: rule %q: Tags entry must be non-empty", name)
		}
		for _, r := range t {
			if r < 0x20 || r == 0x7f {
				return fmt.Errorf("plugin: rule %q: Tags entry %q contains control character", name, t)
			}
		}
	}
	// Version compatibility (optional opt-in).
	if err := checkShinganVersion(m.MinShinganVersion); err != nil {
		return fmt.Errorf("plugin: rule %q: %w", name, err)
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
	// Deep-copy the slice fields so a caller mutating their original
	// slices after Register() can't reach into the registry and change
	// what was validated. Codex round-2 P4 flagged this — without the
	// copy, `m.Frameworks` aliased the caller's slice, leaving a
	// post-validation mutation path that also raced with
	// ListRuleManifests readers.
	stored := m
	stored.Frameworks = copyStrings(m.Frameworks)
	stored.Tags = copyStrings(m.Tags)
	registry = append(registry, Registered{Rule: rule, Manifest: stored})
	return nil
}

// copyStrings returns a defensive copy of s. Returns nil for nil
// input so a manifest with no entries serialises identically.
func copyStrings(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
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
// in registration order. The outer slice and each Manifest's
// Frameworks / Tags slices are deep-copied so callers can mutate the
// result without racing against ongoing Register calls or future
// readers. Codex round-2 P4.
func RegisteredRules() []Registered {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]Registered, len(registry))
	for i, r := range registry {
		copied := r
		copied.Manifest.Frameworks = copyStrings(r.Manifest.Frameworks)
		copied.Manifest.Tags = copyStrings(r.Manifest.Tags)
		out[i] = copied
	}
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

// pluginVersion / setPluginVersion are internal accessors for the
// binary version string. Production code reads `version.Version`
// directly; tests use these to swap the value out and restore it
// without rebuilding the test binary. Defined here (rather than in
// _test.go) so the swap mutates the same global that the production
// `checkShinganVersion` reads.
func pluginVersion() string { return version.Version }

func setPluginVersion(v string) { version.Version = v }
