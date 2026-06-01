// A branching Mastra workflow: classify fans out to handleA / handleB via
// .branch. The branch conditions are JS closures (non-serializable); they are
// NOT statically readable, so the fan-out edges carry an EMPTY condition.
import { createStep, createWorkflow } from "@mastra/core/workflows";

const classify = createStep({
  id: "classify",
  execute: async ({ inputData }) => ({ kind: inputData.kind }),
});

const handleA = createStep({
  id: "handleA",
  execute: async () => ({ done: true }),
});

const handleB = createStep({
  id: "handleB",
  execute: async () => ({ done: true }),
});

export const wf = createWorkflow({ id: "branching-wf" })
  .then(classify)
  .branch([
    [async ({ inputData }) => inputData.kind === "a", handleA],
    [async ({ inputData }) => inputData.kind === "b", handleB],
  ])
  .commit();
