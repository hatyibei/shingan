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
		GeneratedAt: time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC),
		Findings: []domain.FindingFingerprint{
			{RuleName: "cycle_detection", NodeID: "loop_body", Message: "cycle detected"},
			{RuleName: "unreachable_node", NodeID: "orphan", Message: "node unreachable"},
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
