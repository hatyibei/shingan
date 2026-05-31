// NOT Mastra: local helpers happen to be named createStep / createWorkflow but
// are NOT imported from @mastra/*. The parser must NOT mistake this for a
// Mastra workflow (codex review 2026-06-01, P2 false-positive half) — only
// @mastra/* import bindings count.
function createStep(cfg: { id: string }) {
  return cfg;
}

function createWorkflow(_cfg: { id: string }) {
  return {
    then() {
      return this;
    },
    commit() {
      return this;
    },
  };
}

const a = createStep({ id: "a" });
export const wf = createWorkflow({ id: "wf" }).then(a).commit();
