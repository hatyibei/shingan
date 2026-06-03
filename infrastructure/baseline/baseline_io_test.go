package baseline

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hatyibei/shingan/domain"
)

func TestSaveLoad_RoundTrip(t *testing.T) {
	b := &domain.Baseline{
		Version:     domain.BaselineSchemaVersion,
		GeneratedAt: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
		Findings: []domain.FindingFingerprint{
			domain.Fingerprint(domain.Finding{RuleName: "cycle_detection", NodeID: "loop_body", Message: "cycle detected"}),
			domain.Fingerprint(domain.Finding{RuleName: "unreachable_node", NodeID: "orphan", Message: "node unreachable"}),
		},
	}
	path := filepath.Join(t.TempDir(), "baseline.json")

	if err := Save(path, b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if !got.GeneratedAt.Equal(b.GeneratedAt) {
		t.Errorf("GeneratedAt: got %v, want %v", got.GeneratedAt, b.GeneratedAt)
	}
	if len(got.Findings) != len(b.Findings) {
		t.Fatalf("Findings len: got %d, want %d", len(got.Findings), len(b.Findings))
	}
	for i, fp := range got.Findings {
		if fp != b.Findings[i] {
			t.Errorf("Findings[%d]: got %+v, want %+v", i, fp, b.Findings[i])
		}
	}
}

func TestSave_CreatesParentDirs(t *testing.T) {
	b := &domain.Baseline{Findings: []domain.FindingFingerprint{}}
	path := filepath.Join(t.TempDir(), "nested", "subdir", "baseline.json")
	if err := Save(path, b); err != nil {
		t.Fatalf("Save with nested dirs: %v", err)
	}
	if _, err := Load(path); err != nil {
		t.Errorf("Load after Save: %v", err)
	}
}

func TestSave_NilBaseline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	if err := Save(path, nil); err == nil {
		t.Error("Save(nil) should return error")
	}
}

func TestSave_EmptyPath(t *testing.T) {
	if err := Save("", &domain.Baseline{}); err == nil {
		t.Error("Save(empty path) should return error")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	if _, err := Load("/no/such/baseline.json"); err == nil {
		t.Error("Load of missing file should return error")
	}
}

func TestLoad_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Error("Load of malformed JSON should return error")
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Error("Load(empty path) should return error")
	}
}

// TestLoad_BackwardCompat_V1 verifies a legacy v1 baseline (no "version" key,
// fingerprints store the full "message") loads, is migrated to the current
// schema in memory, and its fingerprints still match the original findings.
func TestLoad_BackwardCompat_V1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.json")
	legacy := `{
  "generated_at": "2026-04-15T12:00:00Z",
  "findings": [
    {"rule": "cycle_detection", "node_id": "loop_body", "message": "fan-out: 7 branches"}
  ]
}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load v1: %v", err)
	}
	if got.Version != domain.BaselineSchemaVersion {
		t.Errorf("loaded baseline should be migrated to v%d, got v%d", domain.BaselineSchemaVersion, got.Version)
	}
	// The migrated fingerprint must still suppress the original finding (and one
	// with a drifted numeric value).
	if !got.Contains(domain.Finding{RuleName: "cycle_detection", NodeID: "loop_body", Message: "fan-out: 7 branches"}) {
		t.Error("migrated v1 baseline should still match the original finding")
	}
	if !got.Contains(domain.Finding{RuleName: "cycle_detection", NodeID: "loop_body", Message: "fan-out: 11 branches"}) {
		t.Error("migrated v1 baseline should match a numerically-drifted finding")
	}
}

// TestSave_WritesCurrentVersion verifies Save stamps the current schema version
// even when handed a baseline that was loaded as legacy.
func TestSave_WritesCurrentVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.json")
	b := &domain.Baseline{Findings: []domain.FindingFingerprint{}} // Version 0
	if err := Save(path, b); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Version != domain.BaselineSchemaVersion {
		t.Errorf("saved baseline version = %d, want %d", got.Version, domain.BaselineSchemaVersion)
	}
}
