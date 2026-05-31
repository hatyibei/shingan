// Regression (codex review 2026-05-31, P2): a Command that is CONSTRUCTED but
// never RETURNED must NOT synthesise a control-flow edge. Only a returned
// Command routes the workflow. Harvesting non-returned Commands (a local, a
// log argument, a nested helper) would invent phantom edges and corrupt
// cycle_detection / unreachable_node.
//
//   START -> a   (a constructs Commands but returns plain state; no a->b edge)
import { StateGraph, START, Command } from "@langchain/langgraph";

const graph = new StateGraph({ channels: {} });
graph.addNode("a", a);
graph.addNode("b", b);
graph.addEdge(START, "a");

function a(state: any) {
  const unused = new Command({ goto: "b" }); // constructed, not returned
  const helper = () => new Command({ goto: "b" }); // nested, not the handler's return
  void unused;
  void helper;
  return state;
}

function b(state: any) {
  return state;
}

export const app = graph.compile();
