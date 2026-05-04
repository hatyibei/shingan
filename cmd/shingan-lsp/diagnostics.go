package main

import (
	"fmt"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/hatyibei/shingan/domain"
)

// findingsToDiagnostics converts Shingan findings into LSP diagnostics. The
// graph parameter is optional: when present we look up Node.Pos to drive
// the diagnostic Range; when absent (or when Pos is zero) we fall back to
// (0,0)-(0,1). The fallback is deliberately a 1-character range rather
// than (0,0)-(0,0): some editors hide zero-length ranges entirely.
//
// We never return nil — a non-nil empty slice is required so the LSP
// publish call clears any stale diagnostics from a previous analysis.
func findingsToDiagnostics(graph *domain.WorkflowGraph, findings []domain.Finding) []protocol.Diagnostic {
	out := make([]protocol.Diagnostic, 0, len(findings))
	for _, f := range findings {
		out = append(out, findingToDiagnostic(graph, f))
	}
	return out
}

func findingToDiagnostic(graph *domain.WorkflowGraph, f domain.Finding) protocol.Diagnostic {
	rng := rangeFor(graph, f)

	// We embed the rule name in Code for editors that surface "code"
	// alongside the message (VS Code shows it in the Problems panel).
	// Source is the namespace label that lets users filter our diagnostics
	// from those of other LSP servers running on the same buffer.
	d := protocol.Diagnostic{
		Range:    rng,
		Severity: severityToLSP(f.Severity),
		Code:     f.RuleName,
		Source:   "shingan",
		Message:  buildMessage(f),
	}

	// Data carries the original finding back to the codeAction handler so
	// it can reconstruct fix suggestions without re-running analysis. LSP
	// preserves this field verbatim between publishDiagnostics and
	// codeAction (since 3.16).
	d.Data = map[string]any{
		"node_id":    f.NodeID,
		"rule_name":  f.RuleName,
		"confidence": f.Confidence,
		"suggestion": f.Suggestion,
	}

	return d
}

// rangeFor maps a Finding onto an LSP Range. We try, in order:
//
//  1. If the graph carries the referenced node and the node has a
//     non-zero SourcePos, convert (1-based line/col) → (0-based LSP).
//  2. If neither of the above holds, return (0,0)-(0,1) so the diagnostic
//     still appears prominently in the editor's Problems panel.
//
// We deliberately do not synthesize a multi-line range from heuristics —
// editors render those poorly when wrong, and SourcePos already gives us
// either an exact answer or no answer.
func rangeFor(graph *domain.WorkflowGraph, f domain.Finding) protocol.Range {
	if graph != nil && f.NodeID != "" {
		if node, ok := graph.GetNode(f.NodeID); ok && !node.Pos.IsZero() {
			line := uint32(0)
			col := uint32(0)
			if node.Pos.Line > 0 {
				line = uint32(node.Pos.Line - 1)
			}
			if node.Pos.Col > 0 {
				col = uint32(node.Pos.Col - 1)
			}
			return protocol.Range{
				Start: protocol.Position{Line: line, Character: col},
				End:   protocol.Position{Line: line, Character: col + 1},
			}
		}
	}
	// Fallback: highlight the very first character so the diagnostic
	// still gets attention in the gutter.
	return protocol.Range{
		Start: protocol.Position{Line: 0, Character: 0},
		End:   protocol.Position{Line: 0, Character: 1},
	}
}

// severityToLSP translates Shingan's domain Severity to the LSP scale.
//
// Mapping rationale:
//
//	domain.Critical → DiagnosticSeverityError      — runtime failure or
//	                                                  irreversible damage.
//	domain.Warning  → DiagnosticSeverityWarning    — likely bug, fix-worthy.
//	domain.Info     → DiagnosticSeverityInformation — actionable suggestion.
//
// We never emit DiagnosticSeverityHint: hints render very differently
// across editors (some don't show them at all), and Shingan's Info already
// covers the "softer" tier.
func severityToLSP(s domain.Severity) protocol.DiagnosticSeverity {
	switch s {
	case domain.Critical:
		return protocol.DiagnosticSeverityError
	case domain.Warning:
		return protocol.DiagnosticSeverityWarning
	default:
		return protocol.DiagnosticSeverityInformation
	}
}

// buildMessage assembles the user-facing diagnostic text. We prepend the
// node ID when available so users can correlate the diagnostic with a
// specific graph node, and append a one-line suggestion when present.
func buildMessage(f domain.Finding) string {
	var b strings.Builder
	if f.NodeID != "" {
		fmt.Fprintf(&b, "[%s] ", f.NodeID)
	}
	b.WriteString(f.Message)
	if f.Suggestion != "" {
		b.WriteString(" — ")
		b.WriteString(f.Suggestion)
	}
	return b.String()
}
