package application_test

import (
	"testing"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

const appBenchSeed = 42

// allRules returns the full set of analysis rules used in production.
//
// Now that every rule registers itself via init(), AllBuiltins() returns
// all 15 builtins (cycle, reachability, max_parallel_branches, deprecated_model,
// loop_guard, redundant_llm_call, secret_exposure_scanner, error_handler_checker,
// cost_estimation, pii_leak_scanner, temperature_misuse, model_card_mismatch,
// prompt_injection_sink, eval_missing, dynamic_node_construction) — eight
// more than the pre-refactor bench covered. The 1-walk dispatcher amortises
// the extra rules so the benchmark stays comparable even with the larger rule set.
func allRules() []domain.AnalysisRule {
	return rules.AllBuiltins()
}

// BenchmarkOrchestratorAll benchmarks the concurrent Orchestrator with all 15 rules.
func BenchmarkOrchestratorAll_N10(b *testing.B) {
	runOrchestratorBench(b, 10)
}

func BenchmarkOrchestratorAll_N100(b *testing.B) {
	runOrchestratorBench(b, 100)
}

func BenchmarkOrchestratorAll_N1000(b *testing.B) {
	runOrchestratorBench(b, 1000)
}

func runOrchestratorBench(b *testing.B, n int) {
	b.Helper()
	graph := testutil.GenerateRandomGraph(n, appBenchSeed)
	orch := application.NewAnalysisOrchestrator()
	ruleSet := allRules()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = orch.Analyze(graph, ruleSet)
	}
}

// BenchmarkOrchestratorSequential benchmarks sequential (non-goroutine) execution
// of all 15 rules for direct comparison with the concurrent version.
func BenchmarkOrchestratorSequential_N10(b *testing.B) {
	runSequentialBench(b, 10)
}

func BenchmarkOrchestratorSequential_N100(b *testing.B) {
	runSequentialBench(b, 100)
}

func BenchmarkOrchestratorSequential_N1000(b *testing.B) {
	runSequentialBench(b, 1000)
}

func runSequentialBench(b *testing.B, n int) {
	b.Helper()
	graph := testutil.GenerateRandomGraph(n, appBenchSeed)
	ruleSet := allRules()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var allFindings []domain.Finding
		for _, rule := range ruleSet {
			allFindings = append(allFindings, rule.Analyze(graph)...)
		}
		_ = allFindings
	}
}
