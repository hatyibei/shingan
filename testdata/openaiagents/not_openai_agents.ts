// A .ts file with a LOCAL class named Agent that is NOT imported from
// @openai/agents. The shim gates on the @openai/agents import binding, so this
// must yield an EMPTY graph (no nodes, no edges) — never a false-positive
// handoff graph.
class Agent {
  constructor(public config: { name: string }) {}
}

const a = new Agent({ name: "A" });
const b = new Agent({ name: "B" });

export const agents = [a, b];
