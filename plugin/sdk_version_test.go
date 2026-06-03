package plugin

import (
	"errors"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
)

// versionedStub is a plugin rule that declares a plugin SDK version.
type versionedStub struct {
	name string
	sdk  string
}

func (v versionedStub) Name() string                                   { return v.name }
func (versionedStub) Analyze(_ *domain.WorkflowGraph) []domain.Finding { return nil }
func (v versionedStub) PluginSDKVersion() string                       { return v.sdk }

func registerVersioned(t *testing.T, name, sdk string) {
	t.Helper()
	err := Register(versionedStub{name: name, sdk: sdk}, Manifest{
		Frameworks: []string{"langgraph"},
		Tags:       []string{"company-convention"},
	})
	if err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
}

func TestVerifySDKCompatibility_Compatible(t *testing.T) {
	t.Cleanup(resetForTest)
	registerVersioned(t, "experimental:compat_rule", PluginSDKVersion)
	if err := VerifySDKCompatibility(); err != nil {
		t.Errorf("compatible SDK should pass, got %v", err)
	}
}

func TestVerifySDKCompatibility_TooOld(t *testing.T) {
	t.Cleanup(resetForTest)
	registerVersioned(t, "experimental:old_rule", "0.8.0")
	err := VerifySDKCompatibility()
	if !errors.Is(err, ErrSDKIncompatible) {
		t.Fatalf("expected ErrSDKIncompatible, got %v", err)
	}
}

func TestVerifySDKCompatibility_TooNew(t *testing.T) {
	t.Cleanup(resetForTest)
	registerVersioned(t, "experimental:new_rule", "1.0.0")
	if err := VerifySDKCompatibility(); !errors.Is(err, ErrSDKIncompatible) {
		t.Fatalf("expected ErrSDKIncompatible, got %v", err)
	}
}

func TestVerifySDKCompatibility_InvalidSemver(t *testing.T) {
	t.Cleanup(resetForTest)
	registerVersioned(t, "experimental:bad_rule", "not-a-version")
	if err := VerifySDKCompatibility(); !errors.Is(err, ErrSDKIncompatible) {
		t.Fatalf("expected ErrSDKIncompatible, got %v", err)
	}
}

func TestVerifySDKCompatibility_EmptyDeclared(t *testing.T) {
	t.Cleanup(resetForTest)
	registerVersioned(t, "experimental:empty_rule", "")
	if err := VerifySDKCompatibility(); !errors.Is(err, ErrSDKIncompatible) {
		t.Fatalf("expected ErrSDKIncompatible, got %v", err)
	}
}

// TestVerifySDKCompatibility_OptOut: a rule that does NOT implement
// domain.VersionedRule makes no SDK claim and is skipped.
func TestVerifySDKCompatibility_OptOut(t *testing.T) {
	t.Cleanup(resetForTest)
	if err := Register(stubRule{name: "experimental:plain_rule"}, Manifest{
		Frameworks: []string{"langgraph"},
		Tags:       []string{"company-convention"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := VerifySDKCompatibility(); err != nil {
		t.Errorf("opt-out rule should pass, got %v", err)
	}
}

// TestVerifySDKCompatibility_ListsAllOffenders ensures the aggregated error
// names every incompatible rule, not just the first.
func TestVerifySDKCompatibility_ListsAllOffenders(t *testing.T) {
	t.Cleanup(resetForTest)
	registerVersioned(t, "experimental:old_one", "0.1.0")
	registerVersioned(t, "experimental:new_one", "2.0.0")
	err := VerifySDKCompatibility()
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"experimental:old_one", "experimental:new_one"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q should mention %q", msg, want)
		}
	}
}
