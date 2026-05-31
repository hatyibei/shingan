// A minimal linear Mastra workflow: stepA -> stepB.
// The step ids ("a", "b") come from createStep({ id }); the .then(stepRef)
// references resolve through the variable -> id binding map.
import { createStep, createWorkflow } from "@mastra/core/workflows";

const stepA = createStep({
  id: "a",
  execute: async ({ inputData }) => {
    return { value: inputData.value + 1 };
  },
});

const stepB = createStep({
  id: "b",
  execute: async ({ inputData }) => {
    return { value: inputData.value * 2 };
  },
});

export const wf = createWorkflow({ id: "linear-wf" })
  .then(stepA)
  .then(stepB)
  .commit();
