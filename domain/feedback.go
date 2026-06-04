package domain

import "time"

// FeedbackLabel records whether a human judged a finding a true positive or a
// false positive. It is a string-typed const (mirroring ConfidenceReason) so
// the JSONL value is self-describing and downstream tooling can switch on it.
//
// The labels are deliberately the only two values needed to accrue calibration
// data against stable finding identities; no numeric confidence is stored here
// (that is a future learning-layer concern — see ADR-018).
type FeedbackLabel string

const (
	// LabelTruePositive marks a finding a human confirmed is a real issue.
	LabelTruePositive FeedbackLabel = "tp"
	// LabelFalsePositive marks a finding a human judged a false alarm.
	LabelFalsePositive FeedbackLabel = "fp"
)

// Valid reports whether l is one of the two recognised labels. Callers (the
// infrastructure reader, the CLI ingester) use this to reject malformed input
// rather than silently persisting an unknown label.
func (l FeedbackLabel) Valid() bool {
	switch l {
	case LabelTruePositive, LabelFalsePositive:
		return true
	default:
		return false
	}
}

// FeedbackSource records where a label originated, so future calibration can
// weight or filter by provenance (e.g. trust CI-confirmed labels over ad-hoc
// CLI entries). It does not affect storage semantics in this increment.
type FeedbackSource string

const (
	// SourceCLI is feedback entered via the `shingan feedback` CLI.
	SourceCLI FeedbackSource = "cli"
	// SourceSARIF is feedback derived from a SARIF triage workflow.
	SourceSARIF FeedbackSource = "sarif"
	// SourceAPI is feedback submitted through the HTTP API.
	SourceAPI FeedbackSource = "api"
)

// FeedbackRecord is a single durable true-positive / false-positive label
// against a stable finding identity. It is keyed by the EXISTING
// FindingFingerprint (rule + node_id + source_file + message_digest); Confidence
// is deliberately excluded from that fingerprint, so a recorded label stays
// valid even as a rule's static confidence drifts — exactly the property a
// future calibration layer needs (ADR-018).
//
// Records are persisted as JSONL (one record per line) by the
// infrastructure/feedback package so they append cheaply and accrue over time.
// This type has no I/O methods of its own (Onion principle: domain knows
// nothing about disk).
type FeedbackRecord struct {
	// Fingerprint is the identity of the finding being labelled. It nests its
	// own JSON tags, so a record round-trips the same fingerprint shape a
	// baseline stores — and migrates legacy v1 fingerprints on read for free.
	Fingerprint FindingFingerprint `json:"fingerprint"`
	// Label is the human judgement: "tp" or "fp".
	Label FeedbackLabel `json:"label"`
	// Source records how the label was submitted (cli | sarif | api).
	Source FeedbackSource `json:"source"`
	// Timestamp is when the label was recorded (UTC). It is supplied by the
	// caller (see NewFeedbackRecord) so tests can be deterministic.
	Timestamp time.Time `json:"timestamp"`
}

// NewFeedbackRecord constructs a FeedbackRecord with an injected timestamp.
// The timestamp is a parameter (not a wall-clock read) so callers — and
// especially tests — control it deterministically. The timestamp is normalised
// to UTC for stable on-disk ordering and comparison.
func NewFeedbackRecord(fp FindingFingerprint, label FeedbackLabel, source FeedbackSource, ts time.Time) FeedbackRecord {
	return FeedbackRecord{
		Fingerprint: fp,
		Label:       label,
		Source:      source,
		Timestamp:   ts.UTC(),
	}
}
