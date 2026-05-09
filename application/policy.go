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

	"github.com/hatyibei/shingan/domain"
	"gopkg.in/yaml.v3"
)

// Policy is the parsed `.shingan.yaml` config.
type Policy struct {
	Rules     map[string]RuleConfig  `yaml:"rules,omitempty"`
	Overrides []OverridePolicy       `yaml:"overrides,omitempty"`
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
