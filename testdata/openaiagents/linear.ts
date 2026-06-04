// A minimal linear triage handoff graph: a Triage agent hands off to two
// specialists. The node ids come from each agent's `name` config (normalised:
// "Triage Agent" -> "Triage_Agent"); the handoffs array (a bare agent ref plus
// a `handoff(agent)` wrapper) resolves to declared agents and emits directed
// edges Triage -> Booking and Triage -> Refund. This is the canonical
// Agent.create triage pattern from the @openai/agents docs.
import { Agent, handoff } from "@openai/agents";

const bookingAgent = new Agent({
  name: "Booking Agent",
  instructions: "Help users with booking requests.",
});

const refundAgent = new Agent({
  name: "Refund Agent",
  instructions: "Process refund requests politely and efficiently.",
});

// Agent.create (a CallExpression on the Agent binding) is the documented way to
// keep the finalOutput type aware of handoffs. The shim must recognise it
// alongside `new Agent`.
export const triageAgent = Agent.create({
  name: "Triage Agent",
  instructions: "Route the user to the booking or refund agent.",
  handoffs: [bookingAgent, handoff(refundAgent)],
});
