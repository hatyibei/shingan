package rules_test

import (
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/rules"
	"github.com/hatyibei/shingan/domain/testutil"
)

const benchSeed = 42

// runRuleBench is the shared helper: builds the graph before b.ResetTimer()
// so graph construction cost is excluded from benchmark measurement.
func runRuleBench(b *testing.B, rule domain.AnalysisRule, n int) {
	b.Helper()
	graph := testutil.GenerateRandomGraph(n, benchSeed)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rule.Analyze(graph)
	}
}

// --- cycle_detection ---

func BenchmarkCycleDetection_N10(b *testing.B)   { runRuleBench(b, rules.NewCycleDetector(), 10) }
func BenchmarkCycleDetection_N100(b *testing.B)  { runRuleBench(b, rules.NewCycleDetector(), 100) }
func BenchmarkCycleDetection_N1000(b *testing.B) { runRuleBench(b, rules.NewCycleDetector(), 1000) }

// --- loop_guard ---

func BenchmarkLoopGuard_N10(b *testing.B)   { runRuleBench(b, rules.NewLoopGuardChecker(), 10) }
func BenchmarkLoopGuard_N100(b *testing.B)  { runRuleBench(b, rules.NewLoopGuardChecker(), 100) }
func BenchmarkLoopGuard_N1000(b *testing.B) { runRuleBench(b, rules.NewLoopGuardChecker(), 1000) }

// --- unreachable_node ---

func BenchmarkUnreachableNode_N10(b *testing.B) {
	runRuleBench(b, rules.NewReachabilityChecker(), 10)
}
func BenchmarkUnreachableNode_N100(b *testing.B) {
	runRuleBench(b, rules.NewReachabilityChecker(), 100)
}
func BenchmarkUnreachableNode_N1000(b *testing.B) {
	runRuleBench(b, rules.NewReachabilityChecker(), 1000)
}

// --- error_handler_checker ---

func BenchmarkErrorHandlerChecker_N10(b *testing.B) {
	runRuleBench(b, rules.NewErrorHandlerChecker(), 10)
}
func BenchmarkErrorHandlerChecker_N100(b *testing.B) {
	runRuleBench(b, rules.NewErrorHandlerChecker(), 100)
}
func BenchmarkErrorHandlerChecker_N1000(b *testing.B) {
	runRuleBench(b, rules.NewErrorHandlerChecker(), 1000)
}

// --- cost_estimation ---

func BenchmarkCostEstimation_N10(b *testing.B)   { runRuleBench(b, rules.NewCostAnalyzer(), 10) }
func BenchmarkCostEstimation_N100(b *testing.B)  { runRuleBench(b, rules.NewCostAnalyzer(), 100) }
func BenchmarkCostEstimation_N1000(b *testing.B) { runRuleBench(b, rules.NewCostAnalyzer(), 1000) }

// --- redundant_llm_call ---

func BenchmarkRedundantLLMCall_N10(b *testing.B) {
	runRuleBench(b, rules.NewRedundantLLMDetector(), 10)
}
func BenchmarkRedundantLLMCall_N100(b *testing.B) {
	runRuleBench(b, rules.NewRedundantLLMDetector(), 100)
}
func BenchmarkRedundantLLMCall_N1000(b *testing.B) {
	runRuleBench(b, rules.NewRedundantLLMDetector(), 1000)
}

// --- pii_leak_scanner ---

func BenchmarkPIILeakScanner_N10(b *testing.B)   { runRuleBench(b, rules.NewPIILeakScanner(), 10) }
func BenchmarkPIILeakScanner_N100(b *testing.B)  { runRuleBench(b, rules.NewPIILeakScanner(), 100) }
func BenchmarkPIILeakScanner_N1000(b *testing.B) { runRuleBench(b, rules.NewPIILeakScanner(), 1000) }
