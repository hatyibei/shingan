// Aliased Mastra imports (createStep as makeStep, createWorkflow as
// makeWorkflow) must still be recognised — codex review 2026-06-01 found
// aliased imports silently produced an empty graph. Resolution now keys off
// the @mastra/* import bindings, not the raw callee spelling.
import { createStep as makeStep, createWorkflow as makeWorkflow } from "@mastra/core/workflows";

const a = makeStep({ id: "a", execute: async () => ({}) });
const b = makeStep({ id: "b", execute: async () => ({}) });

export const wf = makeWorkflow({ id: "wf" }).then(a).then(b).commit();
