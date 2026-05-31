// Regression (codex-review parity, 2026-05-31): a router that names a
// destination with NO matching addNode — and no pathMap — must NOT synthesise
// a phantom edge to a non-existent node. Only the END exit (visible via the
// return-type annotation) is materialised, as has_exit_branch on the source.
//
//   START -> x
//   x -> route(): "ghost_a" | "ghost_b" | typeof END   (no pathMap; ghosts undeclared)
import { StateGraph, START, END } from "@langchain/langgraph";

const graph = new StateGraph({ channels: {} });
graph.addNode("x", x);
graph.addEdge(START, "x");
graph.addConditionalEdges("x", route);

function x(state: any) {
  return state;
}

function route(state: any): "ghost_a" | "ghost_b" | typeof END {
  return END;
}

export const app = graph.compile();
