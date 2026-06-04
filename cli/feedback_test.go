package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hatyibei/shingan/domain"
	feedbackio "github.com/hatyibei/shingan/infrastructure/feedback"
)

var fbTS = time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC)

// TestFeedback_SingleAppendByMessage drives the single-record path and verifies
// the persisted JSONL is keyed by a fingerprint EQUAL to domain.Fingerprint of
// the corresponding finding (the central acceptance criterion).
func TestFeedback_SingleAppendByMessage(t *testing.T) {
	store := filepath.Join(t.TempDir(), "labels.jsonl")
	flags := &feedbackFlags{
		store:   store,
		rule:    "loop_guard",
		node:    "agent_a",
		message: "max_iterations not set",
		label:   "fp",
		source:  "cli",
		now:     fbTS,
		out:     &bytes.Buffer{},
		errOut:  &bytes.Buffer{},
	}
	if err := executeFeedback(flags); err != nil {
		t.Fatalf("executeFeedback: %v", err)
	}

	recs, err := feedbackio.Load(store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 1 {
		t.Fatalf("want 1 record, got %d", len(recs))
	}
	got := recs[0]
	if got.Label != domain.LabelFalsePositive {
		t.Errorf("label: got %q want fp", got.Label)
	}
	// The persisted key must equal domain.Fingerprint of the finding.
	want := domain.Fingerprint(domain.Finding{
		RuleName: "loop_guard", NodeID: "agent_a", Message: "max_iterations not set"})
	if got.Fingerprint != want {
		t.Errorf("persisted key %+v != domain.Fingerprint(finding) %+v", got.Fingerprint, want)
	}
	if !got.Timestamp.Equal(fbTS) {
		t.Errorf("timestamp not the injected one: %v", got.Timestamp)
	}
}

// TestFeedback_SingleAppendByDigest covers the explicit-fingerprint path used
// for template-ID findings that can't be reconstructed from analyze JSON.
func TestFeedback_SingleAppendByDigest(t *testing.T) {
	store := filepath.Join(t.TempDir(), "labels.jsonl")
	flags := &feedbackFlags{
		store:  store,
		rule:   "loop_guard",
		node:   "n1",
		digest: "loop_guard.max_iterations_missing",
		label:  "tp",
		source: "cli",
		now:    fbTS,
		out:    &bytes.Buffer{},
		errOut: &bytes.Buffer{},
	}
	if err := executeFeedback(flags); err != nil {
		t.Fatalf("executeFeedback: %v", err)
	}
	recs, err := feedbackio.Load(store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 1 || recs[0].Fingerprint.MessageDigest != "loop_guard.max_iterations_missing" {
		t.Fatalf("explicit digest not persisted verbatim: %+v", recs)
	}
	if recs[0].Label != domain.LabelTruePositive {
		t.Errorf("label: got %q want tp", recs[0].Label)
	}
}

// TestFeedback_IngestFile_Array ingests a JSON array of {finding, label} items
// (the analyze-output shape) and verifies each persisted record's key matches
// domain.Fingerprint of the corresponding finding.
func TestFeedback_IngestFile_Array(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "labels.jsonl")
	ingest := filepath.Join(dir, "triage.json")

	content := `[
	  {"finding": {"rule": "loop_guard", "node_id": "a", "message": "max_iterations not set"}, "label": "fp"},
	  {"finding": {"rule": "cycle_detection", "node_id": "b", "source_file": "wf.py", "message": "cycle detected"}, "label": "tp"},
	  {"fingerprint": {"rule": "r3", "node_id": "n3", "message_digest": "abc123"}, "label": "fp", "source": "sarif"}
	]`
	if err := os.WriteFile(ingest, []byte(content), 0o644); err != nil {
		t.Fatalf("seed ingest file: %v", err)
	}

	flags := &feedbackFlags{store: store, ingest: ingest, now: fbTS, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	if err := executeFeedback(flags); err != nil {
		t.Fatalf("executeFeedback: %v", err)
	}

	recs, err := feedbackio.Load(store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 3 {
		t.Fatalf("want 3 records, got %d", len(recs))
	}

	// Item 0: reconstructed-from-finding key matches domain.Fingerprint.
	want0 := domain.Fingerprint(domain.Finding{RuleName: "loop_guard", NodeID: "a", Message: "max_iterations not set"})
	if recs[0].Fingerprint != want0 || recs[0].Label != domain.LabelFalsePositive {
		t.Errorf("item 0 mismatch: %+v", recs[0])
	}
	// Item 1: source_file participates in the key.
	want1 := domain.Fingerprint(domain.Finding{RuleName: "cycle_detection", NodeID: "b", SourceFile: "wf.py", Message: "cycle detected"})
	if recs[1].Fingerprint != want1 || recs[1].Label != domain.LabelTruePositive {
		t.Errorf("item 1 mismatch: %+v", recs[1])
	}
	// Item 2: explicit fingerprint + non-default source.
	if recs[2].Fingerprint.MessageDigest != "abc123" || recs[2].Source != domain.SourceSARIF {
		t.Errorf("item 2 mismatch: %+v", recs[2])
	}
}

// TestFeedback_IngestFile_JSONL ingests the line-delimited variant.
func TestFeedback_IngestFile_JSONL(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "labels.jsonl")
	ingest := filepath.Join(dir, "triage.jsonl")
	content := `{"finding":{"rule":"r1","node_id":"n1","message":"m1"},"label":"fp"}
{"finding":{"rule":"r2","node_id":"n2","message":"m2"},"label":"tp"}
`
	if err := os.WriteFile(ingest, []byte(content), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	flags := &feedbackFlags{store: store, ingest: ingest, now: fbTS, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	if err := executeFeedback(flags); err != nil {
		t.Fatalf("executeFeedback: %v", err)
	}
	recs, err := feedbackio.Load(store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("want 2 records, got %d", len(recs))
	}
}

// TestFeedback_InvalidLabel rejects a bad label up front, persisting nothing.
func TestFeedback_InvalidLabel(t *testing.T) {
	store := filepath.Join(t.TempDir(), "labels.jsonl")
	flags := &feedbackFlags{store: store, rule: "r", node: "n", message: "m", label: "maybe", now: fbTS, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	if err := executeFeedback(flags); err == nil {
		t.Fatal("invalid label should error")
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Errorf("store should not be created on validation failure, stat err=%v", err)
	}
}

// TestFeedback_IngestFile_BadItemPersistsNothing guards the all-or-nothing
// ingest contract: one invalid item in the batch aborts before any write, so
// the store is never created with a partial batch.
func TestFeedback_IngestFile_BadItemPersistsNothing(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "labels.jsonl")
	ingest := filepath.Join(dir, "triage.json")
	content := `[
	  {"finding": {"rule": "r1", "node_id": "n1", "message": "m1"}, "label": "fp"},
	  {"finding": {"rule": "r2", "node_id": "n2", "message": "m2"}, "label": "bogus"}
	]`
	if err := os.WriteFile(ingest, []byte(content), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	flags := &feedbackFlags{store: store, ingest: ingest, now: fbTS, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	if err := executeFeedback(flags); err == nil {
		t.Fatal("batch with an invalid label should error")
	}
	if _, err := os.Stat(store); !os.IsNotExist(err) {
		t.Errorf("no records should be persisted on batch validation failure, stat err=%v", err)
	}
}

// TestFeedback_MessageAndDigestConflict rejects an ambiguous identity.
func TestFeedback_MessageAndDigestConflict(t *testing.T) {
	store := filepath.Join(t.TempDir(), "labels.jsonl")
	flags := &feedbackFlags{store: store, rule: "r", message: "m", digest: "d", label: "fp", now: fbTS, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
	if err := executeFeedback(flags); err == nil {
		t.Fatal("message+digest together should error")
	}
}

// TestFeedback_EndToEndViaRoot drives the real cobra command surface so the
// subcommand wiring (registration, flag parsing, required --store) is exercised.
func TestFeedback_EndToEndViaRoot(t *testing.T) {
	store := filepath.Join(t.TempDir(), "labels.jsonl")
	root := NewRootCmd()
	silenceErrors(root)
	var out, errBuf bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errBuf)
	root.SetArgs([]string{
		"feedback", "--store", store,
		"--rule", "loop_guard", "--node", "a", "--message", "max_iterations not set",
		"--label", "fp",
	})
	if code := runWithSilencedRoot(root); code != 0 {
		t.Fatalf("feedback via root exit code: got %d want 0; stderr=%q", code, errBuf.String())
	}
	recs, err := feedbackio.Load(store)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(recs) != 1 || recs[0].Label != domain.LabelFalsePositive {
		t.Fatalf("end-to-end record not persisted: %+v", recs)
	}
}

// TestFeedback_MissingStoreFlag confirms --store is required.
func TestFeedback_MissingStoreFlag(t *testing.T) {
	root := NewRootCmd()
	silenceErrors(root)
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"feedback", "--rule", "r", "--message", "m", "--label", "fp"})
	if code := runWithSilencedRoot(root); code == 0 {
		t.Error("missing --store should be a non-zero exit")
	}
}

// TestFeedback_IngestRejectsIncompleteIdentity guards codex #44: an ingest item
// with no rule, or a finding with an empty message (whose digest matches nothing
// real), must abort before any write rather than persist an unkeyed record.
func TestFeedback_IngestRejectsIncompleteIdentity(t *testing.T) {
	cases := []struct{ name, content string }{
		{"finding-no-rule", `[{"finding": {"node_id": "a", "message": "x"}, "label": "fp"}]`},
		{"finding-empty-message", `[{"finding": {"rule": "loop_guard", "node_id": "a", "message": ""}, "label": "fp"}]`},
		{"fingerprint-no-rule", `[{"fingerprint": {"node_id": "n", "message_digest": "abc"}, "label": "fp"}]`},
		{"fingerprint-no-digest", `[{"fingerprint": {"rule": "r", "node_id": "n"}, "label": "fp"}]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store := filepath.Join(dir, "labels.jsonl")
			ingest := filepath.Join(dir, "triage.json")
			if err := os.WriteFile(ingest, []byte(tc.content), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}
			flags := &feedbackFlags{store: store, ingest: ingest, now: fbTS, out: &bytes.Buffer{}, errOut: &bytes.Buffer{}}
			if err := executeFeedback(flags); err == nil {
				t.Fatalf("expected error for incomplete identity, got nil")
			}
			if _, err := os.Stat(store); !os.IsNotExist(err) {
				t.Errorf("store must not be created when ingest aborts (stat err=%v)", err)
			}
		})
	}
}
