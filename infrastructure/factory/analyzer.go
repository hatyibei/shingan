// Package factory provides factory implementations for creating domain objects.
package factory

import (
	"fmt"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
)

// AnalyzerFactory creates AnalysisRule instances by rule type name.
type AnalyzerFactory struct{}

// NewAnalyzerFactory returns a ready-to-use AnalyzerFactory.
func NewAnalyzerFactory() *AnalyzerFactory {
	return &AnalyzerFactory{}
}

// Create returns an AnalysisRule for the given ruleType string.
// Returns an error if ruleType is unknown.
func (f *AnalyzerFactory) Create(ruleType string) (domain.AnalysisRule, error) {
	switch ruleType {
	case "cycle_detection":
		return rules.NewCycleDetector(), nil
	case "unreachable_node":
		return rules.NewReachabilityChecker(), nil
	case "error_handler_checker":
		return rules.NewErrorHandlerChecker(), nil
	case "cost_estimation":
		return rules.NewCostAnalyzer(), nil
	case "redundant_llm_call":
		return rules.NewRedundantLLMDetector(), nil
	case "loop_guard":
		return rules.NewLoopGuardChecker(), nil
	case "pii_leak_scanner":
		return rules.NewPIILeakScanner(), nil
	case "secret_exposure_scanner":
		return rules.NewSecretExposureScanner(), nil
	default:
		return nil, fmt.Errorf("unknown rule type: %q", ruleType)
	}
}

// CreateAll returns all known AnalysisRule instances.
func (f *AnalyzerFactory) CreateAll() []domain.AnalysisRule {
	return []domain.AnalysisRule{
		rules.NewCycleDetector(),
		rules.NewReachabilityChecker(),
		rules.NewErrorHandlerChecker(),
		rules.NewCostAnalyzer(),
		rules.NewRedundantLLMDetector(),
		rules.NewLoopGuardChecker(),
		rules.NewPIILeakScanner(),
		rules.NewSecretExposureScanner(),
	}
}
