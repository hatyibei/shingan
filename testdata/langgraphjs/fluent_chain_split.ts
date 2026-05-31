// Mixed builder style (gap 1): the StateGraph chain is the initializer of a
// variable AND the chain is split across statements — half the nodes/edges are
// declared on the fluent chain (`.addNode("a", ...)`) while the rest use the
// `g` receiver (`g.addNode("b", ...)`). Both halves must resolve to ONE builder
// (`g`), yielding a single complete graph (a + b nodes, START->a->b->END), not
// a split where the chained half lands under a synthetic <anon> builder and is
// dropped by the largest-builder selection.
import { StateGraph, START, END } from "@langchain/langgraph";

const g = new StateGraph({ channels: {} })
  .addNode("a", callA)
  .addEdge(START, "a");

g.addNode("b", callB);
g.addEdge("a", "b");
g.addEdge("b", END);

function callA(state: any) {
  return state;
}
function callB(state: any) {
  return state;
}

export const app = g.compile();
