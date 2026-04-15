// Package application contains use-case logic for Shingan.
// This layer depends only on the domain layer (Onion Architecture).
package application

import (
	"sort"
	"sync"

	"github.com/hatyibei/shingan/domain"
)

// AnalysisOrchestrator runs multiple AnalysisRules concurrently and aggregates
// their findings into a single, sorted slice.
type AnalysisOrchestrator struct{}

// NewAnalysisOrchestrator returns a ready-to-use AnalysisOrchestrator.
func NewAnalysisOrchestrator() *AnalysisOrchestrator {
	return &AnalysisOrchestrator{}
}

// Analyze runs each rule in its own goroutine, collects all findings via a
// buffered channel, and returns them sorted by Severity descending
// (Critical → Warning → Info). Within the same Severity, findings are sorted
// by RuleName ascending for a stable, deterministic order.
//
// An empty or nil rules slice returns an empty (non-nil) slice.
// A nil graph is passed through to each rule as-is; rules that are nil-safe
// will behave normally, and rules that are not will panic — this mirrors the
// behaviour expected by the domain contract (rules must handle nil gracefully).
func (o *AnalysisOrchestrator) Analyze(graph *domain.WorkflowGraph, rules []domain.AnalysisRule) []domain.Finding {
	if len(rules) == 0 {
		return []domain.Finding{}
	}

	findings := make(chan []domain.Finding, len(rules))
	var wg sync.WaitGroup

	for _, rule := range rules {
		wg.Add(1)
		go func(r domain.AnalysisRule) {
			defer wg.Done()
			findings <- r.Analyze(graph)
		}(rule)
	}

	// Close the channel once all goroutines finish so the range below terminates.
	go func() {
		wg.Wait()
		close(findings)
	}()

	var allFindings []domain.Finding
	for batch := range findings {
		allFindings = append(allFindings, batch...)
	}

	// Normalize: a Confidence of 0.0 means "not set by rule" — treat as 1.0
	// for backward compatibility (safe-side default).
	for i := range allFindings {
		if allFindings[i].Confidence == 0.0 {
			allFindings[i].Confidence = 1.0
		}
	}

	// Primary sort: Severity descending (Critical=2 > Warning=1 > Info=0).
	// Secondary sort: Confidence descending (high-confidence findings first).
	// Tertiary sort: RuleName ascending for deterministic output.
	sort.SliceStable(allFindings, func(i, j int) bool {
		if allFindings[i].Severity != allFindings[j].Severity {
			return allFindings[i].Severity > allFindings[j].Severity
		}
		if allFindings[i].Confidence != allFindings[j].Confidence {
			return allFindings[i].Confidence > allFindings[j].Confidence
		}
		return allFindings[i].RuleName < allFindings[j].RuleName
	})

	return allFindings
}
