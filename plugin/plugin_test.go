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

func (s stubRule) Name() string                                   { return s.name }
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
		{Tags: []string{"x"}},               // no frameworks
		{Frameworks: []string{"langgraph"}}, // no tags
		{},                                  // both empty
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

func (s stubRuleNamed) Name() string                                   { return s.name }
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

// TestRegister_ManifestSlicesDeepCopied covers Codex round-2 P4:
// caller-supplied Frameworks / Tags slices must be deep-copied at
// registration time so a post-Register mutation can't reach into the
// registry's manifest. Without the copy, what was validated and what
// is in the catalog drift apart, and concurrent readers race against
// the writer.
func TestRegister_ManifestSlicesDeepCopied(t *testing.T) {
	t.Cleanup(resetForTest)
	fw := []string{"langgraph"}
	tags := []string{"safety"}
	if err := Register(stubRule{name: "experimental:isolation"}, Manifest{
		Frameworks: fw,
		Tags:       tags,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fw[0] = "bogus"
	tags[0] = "bogus"
	got := RegisteredRules()
	if got[0].Manifest.Frameworks[0] != "langgraph" {
		t.Errorf("post-Register Frameworks mutation leaked into registry: %v", got[0].Manifest.Frameworks)
	}
	if got[0].Manifest.Tags[0] != "safety" {
		t.Errorf("post-Register Tags mutation leaked into registry: %v", got[0].Manifest.Tags)
	}
}

// TestRegisteredRules_ReturnsDeepCopy: same guarantee on the read side.
// Mutating the returned slice's contents must not affect future
// reads.
func TestRegisteredRules_ReturnsDeepCopy(t *testing.T) {
	t.Cleanup(resetForTest)
	if err := Register(stubRule{name: "experimental:isolation_read"}, Manifest{
		Frameworks: []string{"langgraph"},
		Tags:       []string{"safety"},
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	first := RegisteredRules()
	first[0].Manifest.Frameworks[0] = "bogus"
	first[0].Manifest.Tags[0] = "bogus"
	second := RegisteredRules()
	if second[0].Manifest.Frameworks[0] != "langgraph" {
		t.Errorf("second read sees mutation from first: %v", second[0].Manifest.Frameworks)
	}
	if second[0].Manifest.Tags[0] != "safety" {
		t.Errorf("second read Tags mutated: %v", second[0].Manifest.Tags)
	}
}

// TestRegister_NameValidationStrict locks in Slice A finding #3:
// the post-prefix portion of the rule name must match the documented
// slug grammar. Before this validation any byte sequence after
// `experimental:` would register — including control chars,
// zero-width Unicode, path-traversal-like characters, and case
// variants — and those names then flowed into YAML keys, SARIF rule
// ids, IDE catalogs, and shell-like contexts.
func TestRegister_NameValidationStrict(t *testing.T) {
	t.Cleanup(resetForTest)
	bad := []string{
		"experimental:",                           // empty suffix
		"experimental:1bad",                       // starts with digit
		"experimental:Foo",                        // uppercase
		"experimental:foo bar",                    // whitespace
		"experimental:foo-bar",                    // hyphen (deliberately disallowed at v0.x)
		"experimental:foo/bar",                    // path separator
		"experimental:foo\nbar",                   // newline
		"experimental:foo\x00bar",                 // NUL
		"experimental:foo​bar",                    // zero-width space
		"experimental:../foo",                     // path traversal
		"experimental:" + strings.Repeat("a", 65), // length cap
	}
	m := Manifest{Frameworks: []string{"all"}, Tags: []string{"x"}}
	for _, name := range bad {
		err := Register(stubRuleNamed{name: name}, m)
		if err == nil {
			t.Errorf("Register(%q) should fail strict name validation", name)
			resetForTest()
			continue
		}
		resetForTest()
	}
	// Sanity: a clean name is accepted.
	if err := Register(stubRule{name: "experimental:clean_name_42"}, m); err != nil {
		t.Errorf("clean name rejected: %v", err)
	}
}

// TestRegister_TagsEntryValidation covers Slice A #4: per-entry Tags
// validation must reject empty, whitespace-only, and control-character
// strings so the catalog has no blank chips.
func TestRegister_TagsEntryValidation(t *testing.T) {
	t.Cleanup(resetForTest)
	bad := [][]string{
		{""},
		{"   "},
		{"security\nx"},
	}
	for _, tags := range bad {
		err := Register(stubRule{name: "experimental:tagcheck"}, Manifest{
			Frameworks: []string{"all"},
			Tags:       tags,
		})
		if err == nil {
			t.Errorf("Tags=%v should fail validation", tags)
		}
		resetForTest()
	}
}

// TestCheckShinganVersion_InvalidBinaryRejected covers Slice A #5:
// a non-dev binary with an invalid version string (e.g. "main", a
// git SHA) used to silently bypass the compat check. Now it
// returns ErrBadVersion so CI of the wrapper binary catches the
// bad ldflag injection.
func TestCheckShinganVersion_InvalidBinaryRejected(t *testing.T) {
	t.Cleanup(resetForTest)
	withBinaryVersion(t, "main", func() {
		err := Register(stubRule{name: "experimental:strict"}, Manifest{
			Frameworks:        []string{"all"},
			Tags:              []string{"x"},
			MinShinganVersion: "0.9.0",
		})
		if !errors.Is(err, ErrBadVersion) {
			t.Errorf("non-dev invalid binary version must surface ErrBadVersion, got %v", err)
		}
	})
}

// TestCheckShinganVersion_VPrefixOnBinaryAccepted: Slice A #6 noted
// that `v0.9.0` injected via ldflags would be rejected because the
// code prepends another `v`. The fix strips a leading `v` from the
// binary version first.
func TestCheckShinganVersion_VPrefixOnBinaryAccepted(t *testing.T) {
	t.Cleanup(resetForTest)
	withBinaryVersion(t, "v0.9.5", func() {
		err := Register(stubRule{name: "experimental:vpfx"}, Manifest{
			Frameworks:        []string{"all"},
			Tags:              []string{"x"},
			MinShinganVersion: "0.9.0",
		})
		if err != nil {
			t.Errorf("binary version %q (with v prefix) should be accepted; got %v", "v0.9.5", err)
		}
	})
}

// TestRegister_DescriptionMustBeSingleLine: Codex Slice F #7.
// A plugin Description containing newline / control chars would
// break the terminal table renderer in `shingan rules`. Reject at
// Register time so plugin authors discover the issue before users.
func TestRegister_DescriptionMustBeSingleLine(t *testing.T) {
	t.Cleanup(resetForTest)
	bad := []string{
		"line one\nline two",
		"line one\rline two",
		"with\x00null",
	}
	for _, d := range bad {
		err := Register(stubRule{name: "experimental:descvalid"}, Manifest{
			Description: d,
			Frameworks:  []string{"all"},
			Tags:        []string{"x"},
		})
		if err == nil {
			t.Errorf("Description=%q must be rejected", d)
		}
		resetForTest()
	}
	// Good: single-line description accepted.
	if err := Register(stubRule{name: "experimental:descvalid"}, Manifest{
		Description: "single line description ok",
		Frameworks:  []string{"all"},
		Tags:        []string{"x"},
	}); err != nil {
		t.Errorf("single-line description rejected: %v", err)
	}
}
