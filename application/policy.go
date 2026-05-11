// Package application — severity-policy support (.shingan.yaml).
//
// Static analyzers earn organizational adoption when teams can express
// "downgrade rule X to Info" / "off rule Y in legacy/" without forking
// Shingan or modifying CI scripts. We support a config file in the
// ESLint / golangci-lint tradition:
//
//	# .shingan.yaml
//	rules:
//	  eval_missing:
//	    severity: critical    # force Critical (default for this rule already)
//	  unbounded_tool_arg:
//	    severity: info        # downgrade to Info
//	  retry_storm:
//	    enabled: false        # disable globally
//	overrides:
//	  - paths:
//	      - "legacy/**"
//	      - "experiments/**"
//	    rules:
//	      cycle_detection:
//	        enabled: false
//	plugins:                   # declarative plugin manifest (v0.9+)
//	  - experimental:todo_node_marker
//	  - experimental:company_naming
//
// The `plugins:` block lists rule names that MUST be registered in the
// running binary's plugin catalog. shingan fails at startup with a
// clear error pointing at the wrapper-binary build instructions if any
// declared plugin is absent. This bridges the gap between Go's static
// linkage (no dynamic plugin loading) and the ESLint-style plugin
// list that teams expect their project config to capture: the YAML
// declares intent, the build pipeline produces a binary that fulfils
// it.
//
// Resolution order (later wins):
//
//	1. Built-in rule defaults (Severity, Confidence)
//	2. Top-level `rules:` overrides
//	3. Matching `overrides[].rules` (path-pattern based)
//
// Findings whose rule has `enabled: false` are dropped before output.

package application

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/hatyibei/shingan/domain"
	"gopkg.in/yaml.v3"
)

// Policy is the parsed `.shingan.yaml` config.
type Policy struct {
	Rules     map[string]RuleConfig `yaml:"rules,omitempty"`
	Overrides []OverridePolicy      `yaml:"overrides,omitempty"`
	// Plugins is the list of plugin rule names the project requires
	// to be present in the running binary's catalog. shingan
	// validates this list at startup via VerifyRequiredPlugins. An
	// empty list (the default) opts out of the check.
	Plugins []string `yaml:"plugins,omitempty"`
}

// RuleConfig overrides one rule's defaults.
type RuleConfig struct {
	// Severity, when non-empty, replaces the rule's default Severity.
	// Accepted values: "critical", "warning", "info", "off".
	// "off" is equivalent to setting Enabled = false.
	Severity string `yaml:"severity,omitempty"`
	// Enabled, when explicitly false, drops every finding from the rule.
	// nil pointer means "leave default" (most rules ship enabled).
	Enabled *bool `yaml:"enabled,omitempty"`
}

// OverridePolicy is a path-scoped policy block. `Paths` accepts standard
// glob patterns matched against `Finding.SourceFile`.
type OverridePolicy struct {
	Paths []string              `yaml:"paths"`
	Rules map[string]RuleConfig `yaml:"rules"`
}

// LoadPolicy reads a YAML policy file. Returns nil for missing/empty
// paths so callers can pass an optional flag.
func LoadPolicy(path string) (*Policy, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read policy %q: %w", path, err)
	}
	var p Policy
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse policy %q: %w", path, err)
	}
	return &p, nil
}

// DiscoverPolicy looks for `.shingan.yaml` (or `.shingan.yml`) walking
// up from `start`. Returns ("", nil) when nothing is found — that's
// not an error; callers can fall back to defaults.
func DiscoverPolicy(start string) (string, error) {
	dir := start
	if dir == "" {
		var err error
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		for _, name := range []string{".shingan.yaml", ".shingan.yml"} {
			candidate := filepath.Join(dir, name)
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", nil
		}
		dir = parent
	}
}

// ApplyPolicy mutates findings in place to reflect Severity overrides
// and drops findings whose rule is disabled. Path-scoped overrides are
// matched against `Finding.SourceFile`. Returns the (possibly shorter)
// slice. A nil policy is a no-op.
func ApplyPolicy(findings []domain.Finding, policy *Policy) []domain.Finding {
	if policy == nil || (len(policy.Rules) == 0 && len(policy.Overrides) == 0) {
		return findings
	}

	out := findings[:0]
	for _, f := range findings {
		// Resolve effective config for this finding (top-level → override).
		effective := mergeRuleConfig(policy.Rules[f.RuleName], policy, f)
		if effective.Enabled != nil && !*effective.Enabled {
			continue
		}
		if sev, ok := parseSeverity(effective.Severity); ok {
			f.Severity = sev
		} else if effective.Severity == "off" {
			continue
		}
		out = append(out, f)
	}
	return out
}

// mergeRuleConfig finds the most-specific RuleConfig for a finding by
// applying path-scoped overrides on top of the global rule config.
func mergeRuleConfig(global RuleConfig, policy *Policy, f domain.Finding) RuleConfig {
	merged := global
	for _, override := range policy.Overrides {
		if !pathMatchesAny(f.SourceFile, override.Paths) {
			continue
		}
		if rc, ok := override.Rules[f.RuleName]; ok {
			if rc.Severity != "" {
				merged.Severity = rc.Severity
			}
			if rc.Enabled != nil {
				v := *rc.Enabled
				merged.Enabled = &v
			}
		}
	}
	return merged
}

func pathMatchesAny(path string, patterns []string) bool {
	if path == "" || len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if matched, _ := filepath.Match(p, path); matched {
			return true
		}
		// Support `**` shorthand: split into prefix+suffix and check.
		if matched := matchDoubleStar(p, path); matched {
			return true
		}
	}
	return false
}

// matchDoubleStar gives shell-glob `**` support. `legacy/**` matches
// `legacy/anything/here.py`; `**/test.py` matches any `test.py`.
func matchDoubleStar(pattern, path string) bool {
	if pattern == "" {
		return false
	}
	// Anchored prefix: `legacy/**` → match anything starting with `legacy/`.
	if filepath.Base(pattern) == "**" {
		prefix := filepath.Dir(pattern)
		if prefix == "." || prefix == "" {
			return true
		}
		return len(path) >= len(prefix) && path[:len(prefix)] == prefix
	}
	// Suffix-anchored: `**/something.py` → match if the path ends in `/something.py`.
	if filepath.Dir(pattern) == "**" {
		suffix := filepath.Base(pattern)
		base := filepath.Base(path)
		if matched, _ := filepath.Match(suffix, base); matched {
			return true
		}
	}
	return false
}

func parseSeverity(s string) (domain.Severity, bool) {
	switch s {
	case "critical":
		return domain.Critical, true
	case "warning":
		return domain.Warning, true
	case "info":
		return domain.Info, true
	}
	return 0, false
}

// experimentalPrefix mirrors plugin.ExperimentalPrefix. Duplicated as
// a constant here so `application` doesn't need to import `plugin`
// at runtime (the test does). Kept in sync via
// TestExperimentalPrefix_MatchesSDK.
const experimentalPrefix = "experimental:"

// validPluginsSuffix mirrors plugin.validNameSuffix. Duplicated so
// .shingan.yaml `plugins:` validation rejects names that the SDK's
// Register would also reject (uppercase, hyphens, whitespace, Unicode,
// path separators, etc.) — Codex Slice C #1: pre-fix, names like
// `experimental:Foo` passed the policy prefix check and surfaced as
// "missing plugin" with a misleading wrapper-build hint, even though
// no plugin could ever register that name. Kept in sync with the SDK
// via TestPluginNameSuffix_MatchesSDK.
var validPluginsSuffix = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// VerifyRequiredPlugins confirms every rule name in policy.Plugins
// is (1) prefixed with `experimental:` (the v0.x SDK contract — only
// plugin rules belong in this list, not built-ins) and (2) present
// in `availableRules`. Returns nil when the policy is nil, has no
// plugins declared, or all declared plugins are valid.
//
// availableRules should be the names of currently-registered plugin
// rules (typically `plugin.Rules()` mapped to names). The caller
// supplies this slice rather than the union of all rules so a
// typo'd built-in name in `.shingan.yaml plugins:` is caught here
// instead of silently passing validation.
//
// Two distinct error categories so users debugging CI know whether
// to fix the YAML or the wrapper binary build:
//
//   - Wrong prefix → typo / misunderstanding; fix the YAML.
//   - Missing from catalog → wrong binary; rebuild the wrapper.
func VerifyRequiredPlugins(policy *Policy, availableRules []string) error {
	if policy == nil || len(policy.Plugins) == 0 {
		return nil
	}
	available := make(map[string]struct{}, len(availableRules))
	for _, name := range availableRules {
		available[name] = struct{}{}
	}
	var badPrefix, badGrammar, badWhitespace, missing []string
	for _, want := range policy.Plugins {
		if want == "" {
			continue
		}
		// Reject leading/trailing whitespace as a policy error
		// rather than silently trimming — Codex Slice C #2. Silent
		// trimming hides typos that the user should know about
		// (a quoted `' experimental:foo '` is almost always a
		// copy/paste accident).
		if strings.TrimSpace(want) != want {
			badWhitespace = append(badWhitespace, want)
			continue
		}
		if !strings.HasPrefix(want, experimentalPrefix) {
			badPrefix = append(badPrefix, want)
			continue
		}
		// Strict grammar check on the post-prefix slug. Same regex
		// as the SDK so policy-level and runtime-level validation
		// converge (Codex Slice C #1). Pre-fix, a name like
		// `experimental:Foo` reached the missing-plugin branch with
		// a misleading wrapper-binary hint, when in reality no
		// plugin could ever register that name.
		suffix := strings.TrimPrefix(want, experimentalPrefix)
		if !validPluginsSuffix.MatchString(suffix) {
			badGrammar = append(badGrammar, want)
			continue
		}
		if _, ok := available[want]; !ok {
			missing = append(missing, want)
		}
	}
	if len(badPrefix) == 0 && len(badGrammar) == 0 && len(badWhitespace) == 0 && len(missing) == 0 {
		return nil
	}
	parts := []string{}
	if len(badWhitespace) > 0 {
		parts = append(parts, fmt.Sprintf(
			"plugins entries must not have leading/trailing whitespace; got (with whitespace shown by quotes): %q",
			badWhitespace,
		))
	}
	if len(badPrefix) > 0 {
		parts = append(parts, fmt.Sprintf(
			"plugins entries must start with %q (built-in rule names belong under `rules:`, not `plugins:`); got: %v",
			experimentalPrefix, badPrefix,
		))
	}
	if len(badGrammar) > 0 {
		parts = append(parts, fmt.Sprintf(
			"plugins entries must match %s after the prefix (no uppercase, hyphens, or path separators); got: %v",
			validPluginsSuffix.String(), badGrammar,
		))
	}
	if len(missing) > 0 {
		parts = append(parts, fmt.Sprintf(
			"plugins declared in .shingan.yaml are not registered in this binary: %v\n"+
				"Hint: build a wrapper binary that side-effect-imports the plugin "+
				"packages. See examples/plugin-template/cmd/shingan-with-plugins/main.go "+
				"or https://github.com/hatyibei/shingan/blob/main/docs/plugin-sdk.md "+
				"for the canonical wrapper.",
			missing,
		))
	}
	return fmt.Errorf("%s", strings.Join(parts, "\n\n"))
}

// RuleNamesFromManifests is a small helper that turns a RuleManifest
// slice into a flat name slice, suitable for passing to
// VerifyRequiredPlugins. Provided so callers don't have to write the
// trivial loop and so the dependency direction stays
// cli → application (cli reads manifests, application validates).
func RuleNamesFromManifests(manifests []RuleManifest) []string {
	out := make([]string, len(manifests))
	for i, m := range manifests {
		out[i] = m.Name
	}
	return out
}
