// Minimal linear LangGraph.js workflow: START -> a -> b -> END.
// Exercises addNode + addEdge + START/END sentinel handling.
import { StateGraph, START, END } from "@langchain/langgraph";

const graph = new StateGraph({ channels: {} })
  .addNode("a", callA)
  .addNode("b", callB)
  .addEdge(START, "a")
  .addEdge("a", "b")
  .addEdge("b", END);

function callA(state: any) {
  return state;
}

function callB(state: any) {
  return state;
}

export const app = graph.compile();
