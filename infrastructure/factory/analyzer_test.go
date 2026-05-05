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

func TestCreateAll_ReturnsFourteenRules(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	all := f.CreateAll()
	if len(all) != 14 {
		t.Fatalf("expected 14 rules, got %d", len(all))
	}
}

func TestCreateAll_ContainsExpectedNames(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	all := f.CreateAll()

	expected := map[string]bool{
		"cycle_detection":         false,
		"unreachable_node":        false,
		"error_handler_checker":   false,
		"cost_estimation":         false,
		"redundant_llm_call":      false,
		"loop_guard":              false,
		"pii_leak_scanner":        false,
		"secret_exposure_scanner": false,
		"max_parallel_branches":   false,
		"deprecated_model":        false,
		"temperature_misuse":      false,
		"model_card_mismatch":     false,
		"prompt_injection_sink":   false,
		"eval_missing":            false,
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

func TestCreate_LoopGuard(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("loop_guard")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.LoopGuardChecker); !ok {
		t.Errorf("expected *rules.LoopGuardChecker, got %T", rule)
	}
	if rule.Name() != "loop_guard" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_PIILeakScanner(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("pii_leak_scanner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.PIILeakScanner); !ok {
		t.Errorf("expected *rules.PIILeakScanner, got %T", rule)
	}
	if rule.Name() != "pii_leak_scanner" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_SecretExposureScanner(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("secret_exposure_scanner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.SecretExposureScanner); !ok {
		t.Errorf("expected *rules.SecretExposureScanner, got %T", rule)
	}
	if rule.Name() != "secret_exposure_scanner" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_MaxParallelBranches(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("max_parallel_branches")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.MaxParallelBranchesChecker); !ok {
		t.Errorf("expected *rules.MaxParallelBranchesChecker, got %T", rule)
	}
	if rule.Name() != "max_parallel_branches" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_DeprecatedModel(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("deprecated_model")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.DeprecatedModelChecker); !ok {
		t.Errorf("expected *rules.DeprecatedModelChecker, got %T", rule)
	}
	if rule.Name() != "deprecated_model" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_TemperatureMisuse(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("temperature_misuse")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.TemperatureMisuseChecker); !ok {
		t.Errorf("expected *rules.TemperatureMisuseChecker, got %T", rule)
	}
	if rule.Name() != "temperature_misuse" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_ModelCardMismatch(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("model_card_mismatch")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.ModelCardMismatchChecker); !ok {
		t.Errorf("expected *rules.ModelCardMismatchChecker, got %T", rule)
	}
	if rule.Name() != "model_card_mismatch" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_PromptInjectionSink(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("prompt_injection_sink")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.PromptInjectionSink); !ok {
		t.Errorf("expected *rules.PromptInjectionSink, got %T", rule)
	}
	if rule.Name() != "prompt_injection_sink" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}

func TestCreate_EvalMissing(t *testing.T) {
	f := factory.NewAnalyzerFactory()
	rule, err := f.Create("eval_missing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := rule.(*rules.EvalMissing); !ok {
		t.Errorf("expected *rules.EvalMissing, got %T", rule)
	}
	if rule.Name() != "eval_missing" {
		t.Errorf("unexpected Name(): %s", rule.Name())
	}
}
