// Aliased @openai/agents imports (`Agent as Ag`, `handoff as h`) must still be
// recognised — the shim keys off the import bindings, not the raw callee
// spelling. Both `new Ag(...)` and `Ag.create(...)` resolve, and `h(b)` unwraps
// to the inner agent. Yields Triage -> Specialist.
import { Agent as Ag, handoff as h } from "@openai/agents";

const specialist = new Ag({
  name: "Specialist",
  instructions: "Handle the specialised task.",
});

export const triage = Ag.create({
  name: "Triage",
  instructions: "Route to the specialist.",
  handoffs: [h(specialist)],
});
