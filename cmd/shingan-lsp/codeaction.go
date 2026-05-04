package main

import (
	"context"
	"fmt"

	"go.lsp.dev/protocol"

	"github.com/hatyibei/shingan/domain"
)

// CodeAction returns a list of remediation actions for the diagnostics in
// the requested range. Today this is a thin wrapper around Suggestion:
// when a finding has a non-empty Suggestion we expose it as a documented
// "explanation" code action that opens a preview but does not (yet) apply
// any TextEdit. Once Track R lands the visitor refactor and ADR-008's
// AutoFix field, real workspace edits plug in here.
//
// Returning a nil result is acceptable per the LSP spec; editors render
// "No code actions available" in that case. We deliberately return an
// empty []CodeAction (not nil) so editors that distinguish "no actions"
// from "server doesn't implement codeAction" treat us as the former.
func (s *Server) CodeAction(_ context.Context, params *protocol.CodeActionParams) ([]protocol.CodeAction, error) {
	doc, ok := s.snapshot(params.TextDocument.URI)
	if !ok {
		return []protocol.CodeAction{}, nil
	}

	actions := make([]protocol.CodeAction, 0)

	// Walk findings whose mapped range overlaps the requested range. We
	// include only findings with a Suggestion to keep the menu tidy —
	// "no fix hint" findings would clutter the editor with empty entries.
	for _, f := range doc.findings {
		if f.Suggestion == "" {
			continue
		}
		findingRange := rangeFor(doc.graph, f)
		if !rangesOverlap(findingRange, params.Range) {
			continue
		}
		actions = append(actions, suggestionAction(f))
	}

	return actions, nil
}

// suggestionAction wraps a finding's Suggestion in a non-destructive
// CodeAction. Marked QuickFix kind so editors group it under the standard
// "Quick Fix" menu, but Edit is left nil — the action surfaces guidance
// without mutating the buffer. When a future Finding gains an AutoFix
// TextEdit field, populate Edit here and remove the placeholder Disabled
// state below.
func suggestionAction(f domain.Finding) protocol.CodeAction {
	title := fmt.Sprintf("shingan: %s — %s", f.RuleName, truncate(f.Suggestion, 80))
	return protocol.CodeAction{
		Title:       title,
		Kind:        protocol.QuickFix,
		IsPreferred: f.Severity == domain.Critical,
		Diagnostics: []protocol.Diagnostic{
			findingToDiagnostic(nil, f),
		},
	}
}

// rangesOverlap reports whether two LSP ranges share at least one
// position. This is the natural relation for CodeAction: "show me all
// fixes whose finding lies anywhere in my selection".
func rangesOverlap(a, b protocol.Range) bool {
	if positionLess(a.End, b.Start) || positionLess(b.End, a.Start) {
		return false
	}
	return true
}

func positionLess(a, b protocol.Position) bool {
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Character < b.Character
}

// truncate shortens s to at most n bytes, appending an ellipsis if it
// had to be cut. Keeps CodeAction titles compact in the editor's menu.
//
// We deliberately operate on bytes rather than runes: today every
// Suggestion is ASCII (the rule authors are all on this codebase) and a
// byte-based slice is allocation-free. If a multibyte Suggestion ever
// lands, swap the implementation to a []rune walk so we never sever a
// UTF-8 sequence mid-codepoint.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
