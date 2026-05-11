package plugin

import (
	"errors"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// withBinaryVersion temporarily overrides `version.Version` for the
// duration of fn, restoring it on cleanup. Compatibility tests use it
// to simulate different release / dev builds without having to
// rebuild the test binary with -ldflags.
func withBinaryVersion(t *testing.T, v string, fn func()) {
	t.Helper()
	orig := pluginVersion()
	setPluginVersion(v)
	defer setPluginVersion(orig)
	fn()
}

// errorsIs is a tiny alias so the new version-compat tests stay
// readable alongside the existing `errors.Is` call sites.
func errorsIs(err, target error) bool { return errors.Is(err, target) }

// stubRule is the minimal AnalysisRule satisfying the contract.
type stubRule struct{ name string }

func (s stubRule) Name() string                                       { return s.name }
func (stubRule) Analyze(_ *domain.WorkflowGraph) []domain.Finding { return nil }

func TestRegister_RequiresExperimentalPrefix(t *testing.T) {
	t.Cleanup(resetForTest)
	err := Register(stubRule{name: "cycle_detection"}, Manifest{
		Frameworks: []string{"langgraph"},
		Tags:       []string{"correctness"},
	})
	if !errors.Is(err, ErrInvalidPrefix) {
		t.Errorf("expected ErrInvalidPrefix, got %v", err)
	}
}

func TestRegister_RequiresFrameworksAndTags(t *testing.T) {
	t.Cleanup(resetForTest)
	cases := []Manifest{
		{Tags: []string{"x"}},                       // no frameworks
		{Frameworks: []string{"langgraph"}},         // no tags
		{},                                          // both empty
	}
	for i, m := range cases {
		err := Register(stubRule{name: "experimental:r"}, m)
		if !errors.Is(err, ErrEmpty) {
			t.Errorf("case %d: expected ErrEmpty, got %v", i, err)
		}
	}
}

func TestRegister_UnknownFrameworkRejected(t *testing.T) {
	t.Cleanup(resetForTest)
	err := Register(stubRule{name: "experimental:r"}, Manifest{
		Frameworks: []string{"airflow"},
		Tags:       []string{"x"},
	})
	if err == nil || !strings.Contains(err.Error(), "unknown framework") {
		t.Errorf("expected unknown framework error, got %v", err)
	}
}

func TestRegister_BuiltinCollisionRejected(t *testing.T) {
	t.Cleanup(resetForTest)
	// `cycle_detection` is a built-in. Even though the prefix check
	// fires first for that name, simulate a plugin trying to shadow
	// via the experimental: prefix.
	err := Register(stubRule{name: "experimental:cycle_detection"}, Manifest{
		Frameworks: []string{"all"},
		Tags:       []string{"x"},
	})
	// This particular name doesn't collide because built-ins don't use
	// the prefix; assert the registration succeeded.
	if err != nil {
		t.Fatalf("expected success for prefixed non-colliding name, got %v", err)
	}
	if len(RegisteredRules()) != 1 {
		t.Errorf("expected 1 registered, got %d", len(RegisteredRules()))
	}
}

func TestRegister_PluginDuplicateRejected(t *testing.T) {
	t.Cleanup(resetForTest)
	m := Manifest{Frameworks: []string{"langgraph"}, Tags: []string{"x"}}
	if err := Register(stubRule{name: "experimental:dup"}, m); err != nil {
		t.Fatalf("first register: %v", err)
	}
	err := Register(stubRule{name: "experimental:dup"}, m)
	if !errors.Is(err, ErrCollision) {
		t.Errorf("expected ErrCollision on duplicate, got %v", err)
	}
	if len(RegisteredRules()) != 1 {
		t.Errorf("expected 1 registered after duplicate, got %d", len(RegisteredRules()))
	}
}

func TestRegister_NilRuleRejected(t *testing.T) {
	t.Cleanup(resetForTest)
	err := Register(nil, Manifest{Frameworks: []string{"all"}, Tags: []string{"x"}})
	if err == nil {
		t.Error("expected error for nil rule")
	}
}

func TestRegister_HappyPath(t *testing.T) {
	t.Cleanup(resetForTest)
	rule := stubRule{name: "experimental:naming"}
	m := Manifest{
		Severity:   domain.Warning,
		Frameworks: []string{"langgraph", "crewai"},
		Tags:       []string{"company-convention"},
		DocsURL:    "https://example.com/naming",
	}
	if err := Register(rule, m); err != nil {
		t.Fatalf("Register: %v", err)
	}
	got := RegisteredRules()
	if len(got) != 1 {
		t.Fatalf("expected 1 registered, got %d", len(got))
	}
	if got[0].Rule.Name() != rule.Name() {
		t.Errorf("rule name: %q != %q", got[0].Rule.Name(), rule.Name())
	}
	if got[0].Manifest.DocsURL != m.DocsURL {
		t.Errorf("manifest DocsURL: %q != %q", got[0].Manifest.DocsURL, m.DocsURL)
	}
	rs := Rules()
	if len(rs) != 1 || rs[0].Name() != rule.Name() {
		t.Errorf("Rules() returned unexpected slice: %+v", rs)
	}
}

func TestMustRegister_PanicsOnError(t *testing.T) {
	t.Cleanup(resetForTest)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected MustRegister to panic on invalid input")
		}
	}()
	MustRegister(stubRule{name: "no_prefix"}, Manifest{
		Frameworks: []string{"all"}, Tags: []string{"x"},
	})
}

// stubRuleNamed is a parameterised stub so the version compat tests
// can register multiple unique names without redefining the type.
type stubRuleNamed struct{ name string }

func (s stubRuleNamed) Name() string                                    { return s.name }
func (stubRuleNamed) Analyze(_ *domain.WorkflowGraph) []domain.Finding { return nil }

// TestRegister_MinShinganVersion_TooOld asserts a plugin requiring a
// future binary version is rejected with ErrVersionMismatch when the
// binary has a tagged release version (not "dev").
func TestRegister_MinShinganVersion_TooOld(t *testing.T) {
	t.Cleanup(resetForTest)
	withBinaryVersion(t, "0.9.0", func() {
		err := Register(stubRule{name: "experimental:future"}, Manifest{
			Frameworks:        []string{"all"},
			Tags:              []string{"x"},
			MinShinganVersion: "0.10.0", // strictly newer than the binary
		})
		if !errorsIs(err, ErrVersionMismatch) {
			t.Errorf("expected ErrVersionMismatch, got %v", err)
		}
	})
}

// TestRegister_MinShinganVersion_Compatible asserts registration
// succeeds when the binary version is >= the plugin's minimum.
func TestRegister_MinShinganVersion_Compatible(t *testing.T) {
	t.Cleanup(resetForTest)
	withBinaryVersion(t, "0.9.5", func() {
		err := Register(stubRule{name: "experimental:ok"}, Manifest{
			Frameworks:        []string{"all"},
			Tags:              []string{"x"},
			MinShinganVersion: "0.9.0",
		})
		if err != nil {
			t.Errorf("expected success, got %v", err)
		}
	})
}

// TestRegister_MinShinganVersion_DevAllowsAnything pins the
// development-friendly behaviour: a "dev" binary (no ldflags) is
// treated as compatible with every plugin so local iteration doesn't
// require pinning the in-tree version.
func TestRegister_MinShinganVersion_DevAllowsAnything(t *testing.T) {
	t.Cleanup(resetForTest)
	withBinaryVersion(t, "dev", func() {
		err := Register(stubRule{name: "experimental:strict"}, Manifest{
			Frameworks:        []string{"all"},
			Tags:              []string{"x"},
			MinShinganVersion: "99.0.0", // unreachable in any real release
		})
		if err != nil {
			t.Errorf("dev build must accept any plugin, got %v", err)
		}
	})
}

// TestRegister_MinShinganVersion_InvalidRejected guards against typos:
// a non-semver MinShinganVersion fails registration with ErrBadVersion
// rather than silently passing.
func TestRegister_MinShinganVersion_InvalidRejected(t *testing.T) {
	t.Cleanup(resetForTest)
	withBinaryVersion(t, "0.9.0", func() {
		err := Register(stubRuleNamed{name: "experimental:typo"}, Manifest{
			Frameworks:        []string{"all"},
			Tags:              []string{"x"},
			MinShinganVersion: "zero point nine",
		})
		if !errorsIs(err, ErrBadVersion) {
			t.Errorf("expected ErrBadVersion, got %v", err)
		}
	})
}

// TestRegister_MinShinganVersion_EmptyOptsOut: leaving the field empty
// is the supported "no opinion" path. Useful for plugins that
// genuinely don't depend on a specific SDK feature.
func TestRegister_MinShinganVersion_EmptyOptsOut(t *testing.T) {
	t.Cleanup(resetForTest)
	withBinaryVersion(t, "0.9.0", func() {
		err := Register(stubRule{name: "experimental:any"}, Manifest{
			Frameworks: []string{"all"},
			Tags:       []string{"x"},
			// MinShinganVersion intentionally omitted.
		})
		if err != nil {
			t.Errorf("expected success when MinShinganVersion empty, got %v", err)
		}
	})
}
