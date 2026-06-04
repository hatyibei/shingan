// dest-must-be-declared gate: a handoff target imported from another module
// (`externalAgent`) does NOT resolve to a declared `new Agent` / `Agent.create`
// binding in THIS file, so the shim emits NO edge and NO phantom node — it omits
// rather than invents (mirroring the LangGraph.js Command-goto gate). Only the
// local `local` agent's handoff to the declared `triage` survives.
import { Agent, handoff } from "@openai/agents";
import { externalAgent } from "./external-agents";

const triage = new Agent({
  name: "Triage",
  instructions: "Local triage agent.",
});

// One declared target (triage) -> edge kept; one imported target
// (externalAgent) -> edge omitted, no phantom node.
const local = new Agent({
  name: "Local",
  instructions: "Hand off locally or to an imported agent.",
  handoffs: [triage, handoff(externalAgent)],
});

export const agents = [triage, local];
