// The load-bearing acceptance fixture (ADR-015): a .dountil loop that the
// workflow CONTINUES PAST. The walk must produce exactly three structural edges:
//
//   start  -> poll       (frontier into the loop)
//   poll   -> poll       (self-loop back-edge: the .dountil repeat)
//   poll   -> finalize   (the post-loop continuation = the STRUCTURAL exit)
//
// Mastra has no in-graph exit sentinel, so cycle bounding is purely structural:
// poll's continuation (poll -> finalize) leaves the {poll} cycle, so
// cycle_detection must downgrade Critical -> Warning via cycleHasExit. A loop
// with NO continuation would stay Critical (documented PoC boundary).
import { createStep, createWorkflow } from "@mastra/core/workflows";

const start = createStep({
  id: "start",
  execute: async () => ({ count: 0 }),
});

const poll = createStep({
  id: "poll",
  execute: async ({ inputData }) => ({ count: inputData.count + 1 }),
});

const finalize = createStep({
  id: "finalize",
  execute: async ({ inputData }) => ({ total: inputData.count }),
});

export const wf = createWorkflow({ id: "loop-wf" })
  .then(start)
  .dountil(poll, async ({ inputData }) => inputData.count >= 5)
  .then(finalize)
  .commit();
