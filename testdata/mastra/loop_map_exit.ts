// Regression fixture for the .dountil(...).map(...) bounded-loop FP
// (wild: hashintel/labs sgai-agent-planner planning-workflow.ts).
//
// The loop's CONTINUATION is a pass-through `.map(reshape)` — reshaping at the
// loop boundary, a common idiom — not a real `.then(after)` step. A pass-through
// emits no node, so there is NO target for a structural exit edge. Without the
// fix the loop step `revise` degenerates to a lone self-loop and cycle.go
// false-Criticals a provably-bounded loop.
//
// The walk must produce:
//   revise -> revise         (self-loop back-edge: the .dountil repeat)
// and flag has_exit_branch=true on `revise` (the pass-through exit has no
// materialisable step id — the sentinel-style case has_exit_branch exists for),
// so cycle_detection downgrades Critical -> Warning (NOT to zero).
import { createStep, createWorkflow } from "@mastra/core/workflows";

const revise = createStep({
  id: "revise",
  execute: async ({ inputData }) => ({
    value: inputData.value,
    attempts: inputData.attempts + 1,
    valid: inputData.valid,
  }),
});

export const wf = createWorkflow({ id: "loop-map-wf" })
  // Entry pass-through: build the loop-compatible shape.
  .map(async ({ inputData }) => ({
    value: inputData.value,
    attempts: 0,
    valid: false,
  }))
  // Bounded revision loop.
  .dountil(revise, async ({ inputData }) => inputData.valid || inputData.attempts >= 3)
  // Exit pass-through: reshape the final result (no resolvable step id).
  .map(async ({ inputData }) => ({ value: inputData.value }))
  .commit();
