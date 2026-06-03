package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"time"
)

// BaselineSchemaVersion is the current on-disk baseline schema version.
//
//	v1 (implicit, no "version" key): fingerprints stored the full Message.
//	v2: fingerprints store a stable MessageDigest instead (ADR-016).
const BaselineSchemaVersion = 2

// Baseline is a snapshot of findings from a previous analysis run, used to
// suppress findings that were already present — enabling progressive adoption
// of Shingan on large existing workflows.
//
// Baselines are persisted as JSON by the infrastructure layer; this type has
// no I/O methods of its own (Onion principle: domain knows nothing about disk).
type Baseline struct {
	// Version is the schema version. Absent/0 or 1 denotes a legacy v1 file
	// (full Message stored); the infrastructure loader migrates those on read
	// and rewrites as the current version on save.
	Version int `json:"version"`
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
// Fields are deliberately a subset of Finding: rule + location + a stable
// message digest. Severity and Confidence are intentionally excluded so that
// re-classifying a rule's severity doesn't invalidate the entire baseline.
//
// MessageDigest replaces the full Message stored by v1 baselines (ADR-016).
// Including the raw Message made baselines brittle: a rule-wording typo fix, a
// numeric value embedded in the message (e.g. "fan-out: 7 branches"), or future
// i18n all invalidated otherwise-identical findings. The digest is either the
// rule's stable MessageTemplateID (when the producing rule implements
// RuleWithMessageTemplate) or the SHA-256 (first 16 hex) of the message with
// numbers and quoted literals normalized away.
//
// SourceFile is included (Codex iter6 P2) so that per-file analysis
// (ADR-012) treats two files producing the same (rule, node_id, message)
// tuple as DISTINCT findings. It is `omitempty`: pre-ADR-012 baselines
// (SourceFile == "") still match against today's findings whose SourceFile
// is also empty (e.g. JSON single-file inputs).
type FindingFingerprint struct {
	RuleName      string `json:"rule"`
	NodeID        string `json:"node_id"`
	SourceFile    string `json:"source_file,omitempty"`
	MessageDigest string `json:"message_digest"`
}

// UnmarshalJSON reads both v2 fingerprints (with "message_digest") and legacy
// v1 fingerprints (with "message"). For a v1 record it derives the digest from
// the stored full message so the fingerprint keeps matching unchanged findings.
func (fp *FindingFingerprint) UnmarshalJSON(data []byte) error {
	type alias FindingFingerprint // break the recursion into UnmarshalJSON
	aux := struct {
		*alias
		LegacyMessage string `json:"message"`
	}{alias: (*alias)(fp)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	// v1 migration: no digest stored, but a legacy full message is present.
	if fp.MessageDigest == "" && aux.LegacyMessage != "" {
		fp.MessageDigest = digestFromMessage(aux.LegacyMessage)
	}
	return nil
}

// RuleWithMessageTemplate is an optional interface a rule may implement to
// provide a stable identifier for its finding message, independent of the
// human-readable wording. When a rule implements it, the orchestrator stamps
// the rule's findings (see Finding.MessageTemplateID) so baseline digests
// survive message-wording changes (typo fixes, i18n). Rules that don't
// implement it fall back to the normalized-message hash.
type RuleWithMessageTemplate interface {
	AnalysisRule
	// MessageTemplateID returns a stable, opaque ID for the rule's message
	// template (e.g. "loop_guard.max_iterations_missing"). It must be stable
	// across releases for baselines to remain valid.
	MessageTemplateID() string
}

// Contains reports whether f matches any fingerprint already in the baseline.
// Match is exact-equality on RuleName, NodeID, SourceFile, and MessageDigest so
// directory-mode analyses (ADR-012) treat per-file findings as distinct.
func (b *Baseline) Contains(f Finding) bool {
	if b == nil {
		return false
	}
	target := Fingerprint(f)
	for _, fp := range b.Findings {
		if fp == target {
			return true
		}
	}
	return false
}

// Fingerprint extracts the identity portion of a Finding for baseline storage.
func Fingerprint(f Finding) FindingFingerprint {
	return FindingFingerprint{
		RuleName:      f.RuleName,
		NodeID:        f.NodeID,
		SourceFile:    f.SourceFile,
		MessageDigest: messageDigest(f),
	}
}

// messageDigest returns the stable digest for a finding's message: the rule's
// MessageTemplateID when one was stamped, otherwise the normalized-message hash.
func messageDigest(f Finding) string {
	if f.MessageTemplateID != "" {
		return f.MessageTemplateID
	}
	return digestFromMessage(f.Message)
}

func digestFromMessage(msg string) string {
	sum := sha256.Sum256([]byte(normalizeMessage(msg)))
	return hex.EncodeToString(sum[:8]) // 16 hex chars
}

var (
	// reQuoted matches single-, double-, or back-quoted literals. Replaced
	// first so any numbers inside a quoted literal are absorbed into [S].
	reQuoted = regexp.MustCompile("'[^']*'|\"[^\"]*\"|`[^`]*`")
	// reNumber matches integer and decimal literals.
	reNumber = regexp.MustCompile(`\d+(?:\.\d+)?`)
)

// normalizeMessage templatizes a message so that values which legitimately vary
// between runs don't change the fingerprint: quoted literals become [S] and
// numeric literals become [N]. "fan-out: 7 branches" and "fan-out: 9 branches"
// both normalize to "fan-out: [N] branches".
func normalizeMessage(s string) string {
	s = reQuoted.ReplaceAllString(s, "[S]")
	s = reNumber.ReplaceAllString(s, "[N]")
	return s
}

// NewBaselineFromFindings builds a Baseline snapshot of the given findings at
// the current time. The returned value is safe to pass to infrastructure I/O.
func NewBaselineFromFindings(findings []Finding) *Baseline {
	fps := make([]FindingFingerprint, 0, len(findings))
	for _, f := range findings {
		fps = append(fps, Fingerprint(f))
	}
	return &Baseline{
		Version:     BaselineSchemaVersion,
		GeneratedAt: time.Now().UTC(),
		Findings:    fps,
	}
}
