package feedback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hatyibei/shingan/domain"
)

// fixedTS is a deterministic timestamp so on-disk bytes are stable across runs.
var fixedTS = time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC)

func rec(rule, node, digest string, label domain.FeedbackLabel) domain.FeedbackRecord {
	return domain.NewFeedbackRecord(
		domain.FindingFingerprint{RuleName: rule, NodeID: node, MessageDigest: digest},
		label, domain.SourceCLI, fixedTS)
}

// TestAppendAndLoad_RoundTrip is the core I/O contract: appended records load
// back in file order with their labels and fingerprints intact.
func TestAppendAndLoad_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.jsonl")

	want := []domain.FeedbackRecord{
		rec("loop_guard", "a", "d1", domain.LabelFalsePositive),
		rec("cycle_detection", "b", "d2", domain.LabelTruePositive),
	}
	for _, r := range want {
		if err := Append(path, r); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("loaded %d records, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("record %d: got %+v want %+v", i, got[i], want[i])
		}
	}
}

// TestAppend_IsJSONL verifies the file is one compact JSON object per line —
// the appendable on-disk format, not pretty-printed JSON.
func TestAppend_IsJSONL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.jsonl")
	if err := Append(path, rec("r1", "n1", "d1", domain.LabelTruePositive)); err != nil {
		t.Fatalf("append 1: %v", err)
	}
	if err := Append(path, rec("r2", "n2", "d2", domain.LabelFalsePositive)); err != nil {
		t.Fatalf("append 2: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	text := string(data)
	if !strings.HasSuffix(text, "\n") {
		t.Errorf("file should end with a newline, got %q", text)
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d: %q", len(lines), text)
	}
	for i, ln := range lines {
		if strings.Contains(ln, "\n") || strings.HasPrefix(ln, "  ") {
			t.Errorf("line %d not compact single-line JSON: %q", i, ln)
		}
	}
	if !strings.Contains(lines[0], `"label":"tp"`) || !strings.Contains(lines[1], `"label":"fp"`) {
		t.Errorf("labels not persisted as expected: %q", text)
	}
}

// TestAppend_CreatesParentDirs mirrors baseline.Save's dir-creation behaviour.
func TestAppend_CreatesParentDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "labels.jsonl")
	if err := Append(path, rec("r", "n", "d", domain.LabelTruePositive)); err != nil {
		t.Fatalf("append into nested dir: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

// TestLoad_SkipsBlankLines tolerates blank lines / trailing newlines.
func TestLoad_SkipsBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.jsonl")
	content := `{"fingerprint":{"rule":"r","node_id":"n","message_digest":"d"},"label":"fp","source":"cli","timestamp":"2026-06-04T09:30:00Z"}

{"fingerprint":{"rule":"r2","node_id":"n2","message_digest":"d2"},"label":"tp","source":"cli","timestamp":"2026-06-04T09:30:00Z"}
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("blank line not skipped: got %d records", len(got))
	}
}

// TestLoad_V1FingerprintMigration confirms a legacy v1 fingerprint (full
// "message" instead of "message_digest") migrates on read for free, because
// the record reuses domain.FindingFingerprint's UnmarshalJSON.
func TestLoad_V1FingerprintMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.jsonl")
	legacy := `{"fingerprint":{"rule":"cycle_detection","node_id":"n1","message":"fan-out: 7 branches"},"label":"fp","source":"cli","timestamp":"2026-06-04T09:30:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 record, got %d", len(got))
	}
	want := domain.Fingerprint(domain.Finding{
		RuleName: "cycle_detection", NodeID: "n1", Message: "fan-out: 7 branches"})
	if got[0].Fingerprint != want {
		t.Errorf("v1 fingerprint did not migrate: got %+v want %+v", got[0].Fingerprint, want)
	}
}

func TestAppend_EmptyPath(t *testing.T) {
	if err := Append("", rec("r", "n", "d", domain.LabelTruePositive)); err == nil {
		t.Error("empty path should error")
	}
}

func TestLoad_EmptyPath(t *testing.T) {
	if _, err := Load(""); err == nil {
		t.Error("empty path should error")
	}
}

func TestLoad_MalformedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "labels.jsonl")
	if err := os.WriteFile(path, []byte("{not json}\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Error("malformed line should error")
	}
}
