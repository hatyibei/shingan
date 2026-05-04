package main

import (
	"context"
	"fmt"
	"strings"

	"go.lsp.dev/protocol"

	"github.com/hatyibei/shingan/domain"
)

// Hover returns rich documentation for the finding closest to the cursor
// position. The "closest" rule is straightforward: any finding whose
// LSP-mapped range contains the cursor wins; ties are broken by severity
// (more severe first) then alphabetically by rule name for determinism.
//
// We return nil (not an error) when no document or no overlapping finding
// is found — the LSP spec treats nil as "no hover available" and editors
// silently dismiss the request.
func (s *Server) Hover(_ context.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	doc, ok := s.snapshot(params.TextDocument.URI)
	if !ok || len(doc.findings) == 0 {
		return nil, nil
	}

	cursor := params.Position
	matches := make([]domain.Finding, 0, len(doc.findings))
	for _, f := range doc.findings {
		r := rangeFor(doc.graph, f)
		if positionInRange(cursor, r) {
			matches = append(matches, f)
		}
	}
	if len(matches) == 0 {
		return nil, nil
	}

	// Sort findings by severity (Critical → Info), then by rule name.
	// We keep this stable so the same hover always returns the same
	// markdown, which is friendlier to LSP clients that diff hovers.
	bestIdx := 0
	for i := 1; i < len(matches); i++ {
		a := matches[i]
		b := matches[bestIdx]
		if a.Severity > b.Severity || (a.Severity == b.Severity && a.RuleName < b.RuleName) {
			bestIdx = i
		}
	}
	primary := matches[bestIdx]

	hoverRange := rangeFor(doc.graph, primary)
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: hoverMarkdown(primary, matches),
		},
		Range: &hoverRange,
	}, nil
}

// hoverMarkdown renders one or more findings as a single markdown block.
// We surface the rule, severity, message, suggestion, and confidence —
// all the data Shingan currently produces. ConfidenceReason (ADR-008) is
// not yet a field on Finding; when it lands, plug it in here.
func hoverMarkdown(primary domain.Finding, all []domain.Finding) string {
	var b strings.Builder

	fmt.Fprintf(&b, "**shingan: %s** _(%s)_\n\n", primary.RuleName, primary.Severity.String())
	if primary.NodeID != "" {
		fmt.Fprintf(&b, "_node: `%s`_\n\n", primary.NodeID)
	}
	b.WriteString(primary.Message)
	b.WriteString("\n\n")

	if primary.Suggestion != "" {
		b.WriteString("**Suggestion:** ")
		b.WriteString(primary.Suggestion)
		b.WriteString("\n\n")
	}

	if primary.Confidence > 0 && primary.Confidence < 1.0 {
		// Render confidence as a percentage with no decimals; LSP
		// markdown renderers handle integer ranges nicely.
		fmt.Fprintf(&b, "_confidence: %.0f%%_\n\n", primary.Confidence*100)
	}

	if len(all) > 1 {
		// Surface co-located findings as a footer so the user sees
		// everything firing at this position without having to wiggle
		// the cursor.
		b.WriteString("---\n\n_Other findings at this position:_\n")
		for _, f := range all {
			if f.RuleName == primary.RuleName {
				continue
			}
			fmt.Fprintf(&b, "- **%s** (%s): %s\n", f.RuleName, f.Severity.String(), f.Message)
		}
	}

	return b.String()
}

// positionInRange reports whether p lies within r (inclusive of start,
// exclusive of end). Mirrors the LSP convention (Range.End is exclusive).
func positionInRange(p protocol.Position, r protocol.Range) bool {
	if p.Line < r.Start.Line || p.Line > r.End.Line {
		return false
	}
	if p.Line == r.Start.Line && p.Character < r.Start.Character {
		return false
	}
	if p.Line == r.End.Line && p.Character >= r.End.Character {
		return false
	}
	return true
}
