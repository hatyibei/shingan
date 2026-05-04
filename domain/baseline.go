package domain

import "time"

// Baseline is a snapshot of findings from a previous analysis run, used to
// suppress findings that were already present — enabling progressive adoption
// of Shingan on large existing workflows.
//
// Baselines are persisted as JSON by the infrastructure layer; this type has
// no I/O methods of its own (Onion principle: domain knows nothing about disk).
type Baseline struct {
	// GeneratedAt records when the baseline was produced.
	GeneratedAt time.Time `json:"generated_at"`
	// Findings is the fingerprint list. Order is stable for round-trip.
	Findings []FindingFingerprint `json:"findings"`
}

// FindingFingerprint is the minimal identity of a Finding for baseline
// comparison. Two Findings with equal fingerprints are considered "the same
// finding" across runs, even if other metadata (confidence, suggestion wording)
// drifts.
//
// Fingerprint fields are deliberately a subset of Finding: rule + location +
// message. Severity and Confidence intentionally excluded so that re-classifying
// a rule's severity doesn't invalidate the entire baseline.
type FindingFingerprint struct {
	RuleName string `json:"rule"`
	NodeID   string `json:"node_id"`
	Message  string `json:"message"`
}

// Contains reports whether f matches any fingerprint already in the baseline.
// Match is exact-equality on RuleName, NodeID, and Message.
func (b *Baseline) Contains(f Finding) bool {
	if b == nil {
		return false
	}
	for _, fp := range b.Findings {
		if fp.RuleName == f.RuleName && fp.NodeID == f.NodeID && fp.Message == f.Message {
			return true
		}
	}
	return false
}

// Fingerprint extracts the identity portion of a Finding for baseline storage.
func Fingerprint(f Finding) FindingFingerprint {
	return FindingFingerprint{
		RuleName: f.RuleName,
		NodeID:   f.NodeID,
		Message:  f.Message,
	}
}

// NewBaselineFromFindings builds a Baseline snapshot of the given findings at
// the current time. The returned value is safe to pass to infrastructure I/O.
func NewBaselineFromFindings(findings []Finding) *Baseline {
	fps := make([]FindingFingerprint, 0, len(findings))
	for _, f := range findings {
		fps = append(fps, Fingerprint(f))
	}
	return &Baseline{
		GeneratedAt: time.Now().UTC(),
		Findings:    fps,
	}
}
