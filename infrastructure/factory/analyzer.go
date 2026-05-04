// Package factory provides factory implementations for creating domain objects.
package factory

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
)

// AnalyzerFactory creates AnalysisRule instances by rule type name.
//
// CreateAll delegates to rules.AllBuiltins() so that adding a new builtin
// rule only requires editing the rule's own init() block — the factory
// stays out of the way (ADR-010 Plugin SDK internal-first).
type AnalyzerFactory struct{}

// NewAnalyzerFactory returns a ready-to-use AnalyzerFactory.
func NewAnalyzerFactory() *AnalyzerFactory {
	return &AnalyzerFactory{}
}

// Create returns an AnalysisRule for the given ruleType string.
// Returns an error if ruleType is unknown.
//
// The lookup walks AllBuiltins() and matches by Name() so the named-lookup
// path stays in sync with the registry without an explicit switch statement.
func (f *AnalyzerFactory) Create(ruleType string) (domain.AnalysisRule, error) {
	for _, r := range rules.AllBuiltins() {
		if r.Name() == ruleType {
			return r, nil
		}
	}
	return nil, fmt.Errorf("unknown rule type: %q", ruleType)
}

// CreateAll returns every builtin rule registered via the rules package's
// init() functions.
func (f *AnalyzerFactory) CreateAll() []domain.AnalysisRule {
	return rules.AllBuiltins()
}
