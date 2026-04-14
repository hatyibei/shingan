// Package main is the entry point for shingan-runner.
//
// shingan-runner integrates Shingan static analysis with ADK-Go runtime execution:
//
//  1. Analyze the sample's source file with Shingan (static analysis).
//  2. If a Critical finding is detected, refuse execution (safe-guard).
//  3. If the agent is clean (or safe-guard is overridden), run it against Vertex AI Gemini.
//
// Usage:
//
//	shingan-runner --sample <name> [--max-iter N] [--dry-run]
//
// Samples:
//
//	simple                   — minimal LlmAgent, says hello in Japanese
//	infinite_loop_bounded    — LoopAgent with MaxIterations=3 (safe)
//	infinite_loop_unbounded  — LoopAgent without MaxIterations (Shingan Critical → refused)
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// newRootCmd builds the root cobra.Command.
func newRootCmd() *cobra.Command {
	var (
		sampleName string
		maxIter    uint
		dryRun     bool
	)

	cmd := &cobra.Command{
		Use:   "shingan-runner",
		Short: "Shingan Runner — analyze then execute ADK-Go agents with Vertex AI",
		Long: `shingan-runner integrates Shingan static analysis with ADK-Go runtime:

  1. Run Shingan on the sample source file (static analysis).
  2. Critical findings → execution refused (safe-guard).
  3. Clean analysis → execute with Vertex AI Gemini.

Examples:
  # Analyze only (no execution)
  shingan-runner --sample simple --dry-run

  # Analyze + execute
  shingan-runner --sample simple

  # Safe-guard demo: Shingan Critical → refused
  shingan-runner --sample infinite_loop_unbounded --dry-run

  # Bounded loop with custom iteration count
  shingan-runner --sample infinite_loop_bounded --max-iter 2`,

		RunE: func(cmd *cobra.Command, args []string) error {
			if sampleName == "" {
				return fmt.Errorf("--sample is required; choose from: simple, infinite_loop_bounded, infinite_loop_unbounded")
			}
			ctx := context.Background()
			return runSample(ctx, sampleName, maxIter, dryRun)
		},
	}

	cmd.Flags().StringVar(&sampleName, "sample", "", "Sample to run: simple | infinite_loop_bounded | infinite_loop_unbounded (required)")
	cmd.Flags().UintVar(&maxIter, "max-iter", 0, "Override MaxIterations for LoopAgent (0 = use default)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Run static analysis only, skip Vertex AI execution")

	_ = cmd.MarkFlagRequired("sample")

	return cmd
}
