// Package main is the entry point for the shingan-gen CLI.
// It generates sample WorkflowGraph JSON files for use with the shingan analyzer.
//
// Usage:
//
//	shingan-gen --pattern <name> --size <N> --seed <S> --output <path|->
//
// Patterns:
//
//	random           — Random graph (may contain intentional bugs)
//	clean            — Structurally correct graph (0 findings expected)
//	buggy            — All 7 rules fire (Critical + Warning findings)
//	infinite-loop    — LoopAgent without max_iterations (loop_guard + cycle_detection)
//	unreachable      — Detached nodes (unreachable_node findings)
//	pii-leak         — RAG→external API without Human gate (pii_leak_scanner)
//	cycle            — Raw cycle without Loop node wrapper (cycle_detection)
//	secret-exposure  — LLM node with hardcoded API key in prompt (secret_exposure_scanner)
//	deprecated-model — LLM node using a shutdown model (deprecated_model Critical)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/domain/testutil"
	"github.com/spf13/cobra"
)

// outputGraph is a JSON-serializable representation of WorkflowGraph that stores
// nodes as an array (matching the format expected by shingan's JSON parser).
// The domain.WorkflowGraph.Nodes field is a map, which would serialize as a JSON
// object; this wrapper converts it to an array for round-trip compatibility.
type outputGraph struct {
	Nodes       []*domain.Node `json:"nodes"`
	Edges       []domain.Edge  `json:"edges"`
	EntryNodeID string         `json:"entry_node_id"`
}

// toOutputGraph converts a domain.WorkflowGraph to the JSON-serializable form,
// sorting nodes by ID for deterministic output.
func toOutputGraph(g *domain.WorkflowGraph) outputGraph {
	nodeSlice := make([]*domain.Node, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		nodeSlice = append(nodeSlice, n)
	}
	sort.Slice(nodeSlice, func(i, j int) bool {
		return nodeSlice[i].ID < nodeSlice[j].ID
	})
	return outputGraph{
		Nodes:       nodeSlice,
		Edges:       g.Edges,
		EntryNodeID: g.EntryNodeID,
	}
}

// generate produces a WorkflowGraph for the given pattern, size, and seed.
func generate(pattern string, size int, seed int64) (*domain.WorkflowGraph, error) {
	switch pattern {
	case "random":
		return testutil.GenerateRandomGraph(size, seed), nil
	case "clean":
		return testutil.GenerateCleanGraph(size, seed), nil
	case "buggy":
		return testutil.GenerateBuggyGraph(seed), nil
	case "infinite-loop":
		return testutil.GenerateInfiniteLoopGraph(seed), nil
	case "unreachable":
		return testutil.GenerateUnreachableGraph(size, seed), nil
	case "pii-leak":
		return testutil.GeneratePIILeakGraph(seed), nil
	case "cycle":
		return testutil.GenerateCycleGraph(size, seed), nil
	case "secret-exposure":
		return testutil.GenerateSecretExposureGraph(seed), nil
	case "deprecated-model":
		return testutil.GenerateDeprecatedModelGraph(seed), nil
	default:
		return nil, fmt.Errorf("unknown pattern %q: must be one of random, clean, buggy, infinite-loop, unreachable, pii-leak, cycle, secret-exposure, deprecated-model", pattern)
	}
}

func main() {
	var pattern string
	var size int
	var seed int64
	var output string

	rootCmd := &cobra.Command{
		Use:   "shingan-gen",
		Short: "Generate sample workflow graphs for Shingan static analysis",
		Long: `shingan-gen produces WorkflowGraph JSON files for use with shingan analyze.

Patterns:
  random          Random graph with intentional bugs (compatible with GenerateRandomGraph)
  clean           Structurally correct graph — 0 findings expected
  buggy           All 7 rules fire: loop_guard, cycle_detection, unreachable_node,
                  error_handler_checker, cost_estimation, redundant_llm_call, pii_leak_scanner
  infinite-loop   LoopAgent without max_iterations — triggers loop_guard + cycle_detection
  unreachable     Detached nodes — triggers unreachable_node
  pii-leak        RAG tool → external API without Human gate — triggers pii_leak_scanner
  cycle           Raw cycle with no Loop node wrapper — triggers cycle_detection
  secret-exposure  LLM node with hardcoded API key in prompt — triggers secret_exposure_scanner (Critical)
  deprecated-model LLM node using a shutdown model — triggers deprecated_model (Critical)

The output is valid JSON compatible with: shingan analyze --format json --input <path>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			graph, err := generate(pattern, size, seed)
			if err != nil {
				return err
			}

			out := toOutputGraph(graph)
			data, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return fmt.Errorf("marshal graph: %w", err)
			}

			if output == "" || output == "-" {
				fmt.Println(string(data))
			} else {
				if err := os.WriteFile(output, data, 0o644); err != nil {
					return fmt.Errorf("write output file %q: %w", output, err)
				}
			}
			return nil
		},
	}

	rootCmd.Flags().StringVar(&pattern, "pattern", "random", "Pattern: random|clean|buggy|infinite-loop|unreachable|pii-leak|cycle|secret-exposure|deprecated-model")
	rootCmd.Flags().IntVar(&size, "size", 10, "Number of nodes (for random/clean/unreachable/cycle patterns)")
	rootCmd.Flags().Int64Var(&seed, "seed", 42, "Random seed for reproducible output")
	rootCmd.Flags().StringVar(&output, "output", "", "Output file path (default: stdout; use '-' for stdout)")

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
