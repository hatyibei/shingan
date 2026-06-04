// A PURE handoff loop: A <-> B with no agent handing off OUT of the cycle.
// OpenAI Agents has NO exit sentinel, so the shim sets no has_exit_branch — the
// downgrade can only come from a STRUCTURAL exit edge, and there is none here.
// cycle_detection must therefore report CRITICAL (an unbounded handoff loop).
//
// Note the forward reference: A's handoffs name `b` before `b` is declared. The
// shim parses the AST only (no binder / type-check), so a forward reference is a
// well-formed AST, and the edge-resolution pass runs AFTER every agent is
// registered — so A->B and B->A both resolve. A static handoff cycle ALWAYS
// needs at least one forward reference (you cannot topologically order a cycle);
// the alternative runtime idiom `a.handoffs.push(b)` is a mutation the AST shim
// deliberately does not read (documented PoC limit).
import { Agent } from "@openai/agents";

const a = new Agent({ name: "A", handoffs: [b] });
const b = new Agent({ name: "B", handoffs: [a] });

export const agents = [a, b];
