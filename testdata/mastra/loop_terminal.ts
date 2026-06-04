// Discriminating fixture: a genuinely chain-TERMINAL loop —
// `.dountil(poll, cond).commit()` with NO continuation after the loop.
//
// This is the boundary the .dountil(...).map(...) fix must NOT cross: there is
// no pass-through (or real step) after the loop, so we invent NO exit edge and
// set NO has_exit_branch. The lone self-loop {poll} stays genuinely exit-less,
// so cycle_detection correctly keeps Critical (documented PoC boundary, mirrors
// AutoGen). Locks the narrowness guarantee: the fix fires ONLY on a pass-through
// continuation, never on a bare terminal loop.
import { createStep, createWorkflow } from "@mastra/core/workflows";

const poll = createStep({
  id: "poll",
  execute: async ({ inputData }) => ({ count: inputData.count + 1 }),
});

export const wf = createWorkflow({ id: "terminal-loop-wf" })
  .dountil(poll, async ({ inputData }) => inputData.count >= 5)
  .commit();
