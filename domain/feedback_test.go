package domain

import (
	"encoding/json"
	"testing"
	"time"
)

func TestFeedbackLabel_Valid(t *testing.T) {
	cases := []struct {
		label FeedbackLabel
		want  bool
	}{
		{LabelTruePositive, true},
		{LabelFalsePositive, true},
		{"TP", false},
		{"", false},
		{"maybe", false},
	}
	for _, tc := range cases {
		if got := tc.label.Valid(); got != tc.want {
			t.Errorf("FeedbackLabel(%q).Valid()=%v, want %v", tc.label, got, tc.want)
		}
	}
}

// TestNewFeedbackRecord_NormalisesUTC verifies the injected timestamp is stored
// as UTC so on-disk records are comparable regardless of the caller's location.
func TestNewFeedbackRecord_NormalisesUTC(t *testing.T) {
	loc := time.FixedZone("JST", 9*3600)
	ts := time.Date(2026, 6, 4, 12, 0, 0, 0, loc)
	fp := FindingFingerprint{RuleName: "r", NodeID: "n", MessageDigest: "deadbeef"}

	rec := NewFeedbackRecord(fp, LabelFalsePositive, SourceCLI, ts)

	if rec.Timestamp.Location() != time.UTC {
		t.Errorf("timestamp not normalised to UTC: %v", rec.Timestamp.Location())
	}
	if !rec.Timestamp.Equal(ts) {
		t.Errorf("timestamp instant changed: got %v want %v", rec.Timestamp, ts)
	}
}

// TestFeedbackRecord_RoundTripJSONL is the core record contract: a record
// marshals to a single JSON line and unmarshals back identically, and a
// tp/fp label survives the trip.
func TestFeedbackRecord_RoundTripJSONL(t *testing.T) {
	ts := time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC)
	for _, label := range []FeedbackLabel{LabelTruePositive, LabelFalsePositive} {
		fp := FindingFingerprint{
			RuleName:      "loop_guard",
			NodeID:        "agent_a",
			SourceFile:    "wf.py",
			MessageDigest: "0a1b2c3d4e5f6071",
		}
		rec := NewFeedbackRecord(fp, label, SourceSARIF, ts)

		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// JSONL invariant: a record must serialize to a single line.
		if len(data) == 0 || containsNewline(data) {
			t.Fatalf("record JSON must be a single line, got %q", data)
		}

		var got FeedbackRecord
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.Label != label {
			t.Errorf("label round-trip: got %q want %q", got.Label, label)
		}
		if got.Source != SourceSARIF {
			t.Errorf("source round-trip: got %q want %q", got.Source, SourceSARIF)
		}
		if got.Fingerprint != fp {
			t.Errorf("fingerprint round-trip: got %+v want %+v", got.Fingerprint, fp)
		}
		if !got.Timestamp.Equal(ts) {
			t.Errorf("timestamp round-trip: got %v want %v", got.Timestamp, ts)
		}
	}
}

// TestFeedbackRecord_FingerprintMatchesFinding proves the persisted key equals
// domain.Fingerprint of the corresponding finding — the property the future
// calibration layer depends on (ADR-018).
func TestFeedbackRecord_FingerprintMatchesFinding(t *testing.T) {
	f := Finding{
		RuleName:   "max_parallel_branches",
		NodeID:     "fanout",
		SourceFile: "graph.py",
		Message:    "fan-out: 7 branches",
		Confidence: 0.5, // deliberately set: must NOT affect the key
	}
	rec := NewFeedbackRecord(Fingerprint(f), LabelTruePositive, SourceCLI, time.Unix(0, 0))

	if rec.Fingerprint != Fingerprint(f) {
		t.Errorf("record key %+v != Fingerprint(finding) %+v", rec.Fingerprint, Fingerprint(f))
	}
	// Confidence drift must not invalidate the label: a later finding with a
	// different confidence (and a numerically-drifted message) maps to the
	// same key.
	drift := Finding{
		RuleName: "max_parallel_branches", NodeID: "fanout", SourceFile: "graph.py",
		Message: "fan-out: 12 branches", Confidence: 0.9,
	}
	if rec.Fingerprint != Fingerprint(drift) {
		t.Errorf("label key should survive confidence/numeric drift: %+v vs %+v",
			rec.Fingerprint, Fingerprint(drift))
	}
}

func containsNewline(b []byte) bool {
	for _, c := range b {
		if c == '\n' {
			return true
		}
	}
	return false
}
