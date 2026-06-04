package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hatyibei/shingan/domain"
	feedbackio "github.com/hatyibei/shingan/infrastructure/feedback"
	"github.com/spf13/cobra"
)

// feedbackFlags holds the parsed flags for `shingan feedback`.
type feedbackFlags struct {
	store  string // --store=<path>: JSONL file to append to / load from
	ingest string // --ingest=<path>: a JSON/JSONL file of {finding|fingerprint, label}

	// Single-record append. Identity is given EITHER as raw finding fields
	// (rule/node/source-file/message → domain.Fingerprint) OR as an explicit
	// message digest (--digest), but not a mix that conflicts.
	rule       string
	node       string
	sourceFile string
	message    string
	digest     string
	label      string
	source     string

	// now is the timestamp stamped on appended records. Injected so tests are
	// deterministic; defaults to time.Now().UTC() when zero.
	now time.Time
	// out / errOut writers, threaded from cobra for testability.
	out    io.Writer
	errOut io.Writer
}

// feedbackIngestItem is the CLI-layer DTO for a line/element in an --ingest
// file. A user may identify the finding two ways, checked in this order:
//
//  1. an embedded "fingerprint" object (rule/node_id/source_file/message_digest)
//     — the exact key the store uses; or
//  2. an embedded "finding" object carrying the analyze JSON shape
//     (rule/node_id/source_file/message), from which the fingerprint is derived
//     via domain.Fingerprint.
//
// The DTO and its mapping live in the cli layer (not domain): the file format
// is a UX concern, and keeping the domain.Finding literal out of domain/rules
// avoids the check-confidence-reason linter (which only scans ./domain/rules).
type feedbackIngestItem struct {
	Fingerprint *domain.FindingFingerprint `json:"fingerprint,omitempty"`
	Finding     *ingestFinding             `json:"finding,omitempty"`
	Label       domain.FeedbackLabel       `json:"label"`
	Source      domain.FeedbackSource      `json:"source,omitempty"`
}

// ingestFinding is the subset of the analyze JSON finding shape needed to
// reconstruct a fingerprint. It mirrors infrastructure/reporter's jsonFinding
// field names so a user can feed back directly against `analyze --output json`.
//
// LIMITATION (documented in docs/feedback.md): the analyze JSON reporter emits
// "message" but NOT "message_template_id". For ordinary rules the fingerprint
// is reconstructed exactly (digest = hash of the normalized message). For rules
// implementing RuleWithMessageTemplate, the live finding's digest is the
// template ID, which is absent from analyze output — so a fingerprint rebuilt
// from JSON would hash the message instead and NOT match. Such findings should
// be fed back via an explicit "fingerprint" object (or --digest).
type ingestFinding struct {
	Rule       string `json:"rule"`
	NodeID     string `json:"node_id"`
	SourceFile string `json:"source_file,omitempty"`
	Message    string `json:"message"`
}

// fingerprint resolves the ingest item to a domain.FindingFingerprint, or an
// error if neither identifier is present.
func (it feedbackIngestItem) fingerprint() (domain.FindingFingerprint, error) {
	if it.Fingerprint != nil {
		return *it.Fingerprint, nil
	}
	if it.Finding != nil {
		return fingerprintFromFinding(
			it.Finding.Rule, it.Finding.NodeID, it.Finding.SourceFile, it.Finding.Message), nil
	}
	return domain.FindingFingerprint{},
		fmt.Errorf("item has neither a \"fingerprint\" nor a \"finding\" object")
}

// fingerprintFromFinding maps the analyze-output fields of a finding to its
// fingerprint via the canonical domain.Fingerprint, so the persisted key is
// identical to a baseline entry / live finding. Building the domain.Finding
// literal HERE (cli) rather than in domain/rules keeps it clear of the
// check-confidence-reason linter.
func fingerprintFromFinding(rule, node, sourceFile, message string) domain.FindingFingerprint {
	return domain.Fingerprint(domain.Finding{
		RuleName:   rule,
		NodeID:     node,
		SourceFile: sourceFile,
		Message:    message,
	})
}

// newFeedbackCmd builds the `shingan feedback` subcommand: a capture-only store
// for true-positive / false-positive labels keyed by the stable finding
// fingerprint. It either appends one record (from flags) or ingests a batch
// from a JSON/JSONL file. Nothing reads these labels back into analysis in this
// increment — see ADR-018 and docs/feedback.md.
func newFeedbackCmd() *cobra.Command {
	flags := &feedbackFlags{}

	cmd := &cobra.Command{
		Use:   "feedback",
		Short: "Record true-positive / false-positive labels for findings (capture-only)",
		Long: `feedback appends durable (tp|fp) labels for findings to a JSONL store,
keyed by the same stable fingerprint baselines use
(rule + node_id + source_file + message_digest). Confidence is intentionally
NOT part of the key, so labels stay valid as a rule's static confidence drifts.

This is capture-only: it never changes ` + "`shingan analyze`" + ` output. The labels
accrue until volume justifies a future calibration layer (see ADR-018).

Two modes:

  # Append one label from explicit finding fields
  shingan feedback --store labels.jsonl \
      --rule loop_guard --node agent_a --message "max_iterations not set" \
      --label fp

  # Ingest a batch: a JSON array OR JSONL of {finding|fingerprint, label}
  shingan feedback --store labels.jsonl --ingest triage.json

The --ingest file accepts items shaped like the analyze JSON finding
(a "finding" object: rule/node_id/source_file/message) — so you can feed back
directly against ` + "`shingan analyze --output json`" + ` — or an explicit
"fingerprint" object for findings whose digest is a rule message-template ID.`,
		Example: `  # Single false positive by finding fields
  shingan feedback --store labels.jsonl --rule loop_guard --node a --message "m" --label fp

  # Single true positive by explicit digest
  shingan feedback --store labels.jsonl --rule r --node n --digest 0a1b2c3d4e5f6071 --label tp

  # Batch from analyze output you triaged
  shingan feedback --store labels.jsonl --ingest triaged.json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			flags.out = cmd.OutOrStdout()
			flags.errOut = cmd.ErrOrStderr()
			return executeFeedback(flags)
		},
	}

	cmd.Flags().StringVar(&flags.store, "store", "", "Path to the JSONL feedback store to append to (required)")
	cmd.Flags().StringVar(&flags.ingest, "ingest", "", "Path to a JSON array / JSONL file of {finding|fingerprint, label} to ingest")
	cmd.Flags().StringVar(&flags.rule, "rule", "", "Rule name of the finding (single-record mode)")
	cmd.Flags().StringVar(&flags.node, "node", "", "Node ID of the finding (single-record mode)")
	cmd.Flags().StringVar(&flags.sourceFile, "source-file", "", "Source file of the finding (single-record mode; optional)")
	cmd.Flags().StringVar(&flags.message, "message", "", "Finding message; its digest is derived via domain.Fingerprint (single-record mode)")
	cmd.Flags().StringVar(&flags.digest, "digest", "", "Explicit message digest, instead of --message (single-record mode)")
	cmd.Flags().StringVar(&flags.label, "label", "", "Label to record: tp (true positive) or fp (false positive)")
	cmd.Flags().StringVar(&flags.source, "source", string(domain.SourceCLI), "Provenance of the label: cli, sarif, or api")

	_ = cmd.MarkFlagRequired("store")

	return cmd
}

// executeFeedback runs the feedback command: ingest a batch, or append one
// record. It validates inputs up front so a bad label/identity fails fast with
// a clear message rather than persisting garbage.
func executeFeedback(flags *feedbackFlags) error {
	if flags.store == "" {
		return fmt.Errorf("--store is required")
	}
	now := flags.now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	if flags.ingest != "" {
		n, err := ingestFeedbackFile(flags, now)
		if err != nil {
			return err
		}
		fmt.Fprintf(flags.errOut, "shingan feedback: ingested %d record(s) into %s\n", n, flags.store)
		return nil
	}

	// Single-record append mode.
	rec, err := buildSingleRecord(flags, now)
	if err != nil {
		return err
	}
	if err := feedbackio.Append(flags.store, rec); err != nil {
		return fmt.Errorf("append feedback: %w", err)
	}
	fmt.Fprintf(flags.errOut, "shingan feedback: recorded %s for %s/%s into %s\n",
		rec.Label, rec.Fingerprint.RuleName, rec.Fingerprint.NodeID, flags.store)
	return nil
}

// buildSingleRecord assembles one FeedbackRecord from the single-record flags,
// validating the label, source, and identity.
func buildSingleRecord(flags *feedbackFlags, now time.Time) (domain.FeedbackRecord, error) {
	label := domain.FeedbackLabel(flags.label)
	if !label.Valid() {
		return domain.FeedbackRecord{},
			fmt.Errorf("--label must be tp or fp, got %q", flags.label)
	}
	// Source is intentionally NOT validated against the cli/sarif/api set:
	// it is free-form provenance metadata for a capture-only store, and being
	// permissive here avoids rejecting a future source the learning layer might
	// introduce. Only the load-bearing Label is strictly checked.
	source := domain.FeedbackSource(flags.source)

	if flags.message != "" && flags.digest != "" {
		return domain.FeedbackRecord{},
			fmt.Errorf("pass either --message or --digest, not both")
	}

	var fp domain.FindingFingerprint
	switch {
	case flags.digest != "":
		if flags.rule == "" {
			return domain.FeedbackRecord{}, fmt.Errorf("--rule is required with --digest")
		}
		fp = domain.FindingFingerprint{
			RuleName:      flags.rule,
			NodeID:        flags.node,
			SourceFile:    flags.sourceFile,
			MessageDigest: flags.digest,
		}
	case flags.message != "":
		if flags.rule == "" {
			return domain.FeedbackRecord{}, fmt.Errorf("--rule is required with --message")
		}
		fp = fingerprintFromFinding(flags.rule, flags.node, flags.sourceFile, flags.message)
	default:
		return domain.FeedbackRecord{},
			fmt.Errorf("provide --message or --digest to identify the finding")
	}

	return domain.NewFeedbackRecord(fp, label, source, now), nil
}

// ingestFeedbackFile reads a JSON array OR a JSONL stream of feedbackIngestItem,
// converts each to a FeedbackRecord, and appends them to the store. It returns
// the number of records persisted. The whole batch is validated before any
// write so a malformed item aborts cleanly rather than half-persisting.
func ingestFeedbackFile(flags *feedbackFlags, now time.Time) (int, error) {
	data, err := os.ReadFile(flags.ingest)
	if err != nil {
		return 0, fmt.Errorf("read ingest file %q: %w", flags.ingest, err)
	}

	items, err := parseIngestItems(data)
	if err != nil {
		return 0, fmt.Errorf("parse ingest file %q: %w", flags.ingest, err)
	}

	records := make([]domain.FeedbackRecord, 0, len(items))
	for i, it := range items {
		if !it.Label.Valid() {
			return 0, fmt.Errorf("item %d: label must be tp or fp, got %q", i, it.Label)
		}
		fp, ferr := it.fingerprint()
		if ferr != nil {
			return 0, fmt.Errorf("item %d: %w", i, ferr)
		}
		source := it.Source
		if source == "" {
			source = domain.SourceCLI
		}
		records = append(records, domain.NewFeedbackRecord(fp, it.Label, source, now))
	}

	if err := feedbackio.AppendAll(flags.store, records); err != nil {
		return 0, fmt.Errorf("persist ingested feedback: %w", err)
	}
	return len(records), nil
}

// parseIngestItems accepts either a JSON array of items (the whole file is one
// `[...]`) or JSONL (one item object per line). It sniffs the first
// non-whitespace byte: '[' → array, otherwise line-delimited.
func parseIngestItems(data []byte) ([]feedbackIngestItem, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if trimmed[0] == '[' {
		var items []feedbackIngestItem
		if err := json.Unmarshal(trimmed, &items); err != nil {
			return nil, fmt.Errorf("decode JSON array: %w", err)
		}
		return items, nil
	}

	var items []feedbackIngestItem
	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytes.TrimSpace(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		var it feedbackIngestItem
		if err := json.Unmarshal(raw, &it); err != nil {
			return nil, fmt.Errorf("decode JSONL line %d: %w", line, err)
		}
		items = append(items, it)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan JSONL: %w", err)
	}
	return items, nil
}
