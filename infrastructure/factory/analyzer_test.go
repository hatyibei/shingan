package factory_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/infrastructure/factory"
)

func TestCreate_CycleDetection(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("cycle_detection")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.CycleDetector); !ok {
		t.Errorf("expected *rules.CycleDetector, got %T", rule)
	}
	if rule.Name() != "cycle_detection" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_UnreachableNode(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("unreachable_node")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.ReachabilityChecker); !ok {
		t.Errorf("expected *rules.ReachabilityChecker, got %T", rule)
	}
	if rule.Name() != "unreachable_node" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_ErrorHandlerChecker(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("error_handler_checker")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.ErrorHandlerChecker); !ok {
		t.Errorf("expected *rules.ErrorHandlerChecker, got %T", rule)
	}
	if rule.Name() != "error_handler_checker" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_UnknownRuleReturnsError(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("nonexistent_rule")
	if err == nil {
		t.Fatal("expected error for unknown rule type, got nil")
	}
	if rule != nil {
		t.Errorf("expected nil rule on error, got %T", rule)
	}
}

func TestCreate_EmptyStringReturnsError(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	_, err := f.Create("")
	if err == nil {
		t.Fatal("expected error for empty rule type, got nil")
	}
}

func TestCreate_CostEstimation(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("cost_estimation")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.CostAnalyzer); !ok {
		t.Errorf("expected *rules.CostAnalyzer, got %T", rule)
	}
	if rule.Name() != "cost_estimation" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_RedundantLLMCall(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("redundant_llm_call")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.RedundantLLMDetector); !ok {
		t.Errorf("expected *rules.RedundantLLMDetector, got %T", rule)
	}
	if rule.Name() != "redundant_llm_call" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreateAll_ReturnsFiveRules(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	all := f.CreateAll()
	if len(all) != 5 {
		t.Fatalf("expected 5 rules, got %d", len(all))
	}
}

func TestCreateAll_ContainsExpectedNames(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	all := f.CreateAll()

	expected := map[string]bool{
		"cycle_detection":       false,
		"unreachable_node":      false,
		"error_handler_checker": false,
		"cost_estimation":       false,
		"redundant_llm_call":    false,
	}

	for _, rule := range all {
		if _, ok := expected[rule.Name()]; !ok {
			t.Errorf("unexpected rule name: %s", rule.Name())
		}
		expected[rule.Name()] = true
	}

	for name, found := range expected {
		if !found {
			t.Errorf("missing expected rule: %s", name)
		}
	}
}
