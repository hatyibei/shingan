// A handoff loop A <-> B where an IN-CYCLE agent (A) also hands off OUT of the
// cycle to a terminal agent C. The cycle is {A, B}; A->C leaves the cycle, so
// domain/rules/cycle.go `cycleHasExit` sees a structural exit and downgrades
// cycle_detection from Critical to WARNING — WITHOUT any exit sentinel (OpenAI
// Agents has none; has_exit_branch stays false).
//
// The exit edge must originate from an in-cycle node (A here) for the downgrade
// to apply; an edge from C (a terminal, out-of-cycle agent) would not.
import { Agent } from "@openai/agents";

// Terminal specialist — no handoffs, so the run ends here implicitly.
const c = new Agent({ name: "C", instructions: "Finalize and answer." });

// A is in the {A,B} cycle but ALSO hands off to the terminal C (the exit).
const a = new Agent({ name: "A", handoffs: [b, c] });

const b = new Agent({ name: "B", handoffs: [a] });

export const agents = [a, b, c];
