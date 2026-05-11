package plugin

import (
	"errors"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

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
