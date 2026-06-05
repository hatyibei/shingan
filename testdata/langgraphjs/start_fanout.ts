// START fan-out: START routes to MULTIPLE entry nodes that run in parallel —
// here three plain `addEdge(START, X)` edges plus a conditional-from-START that
// adds a fourth. LangGraph.js executes every START successor concurrently; the
// AST-only shim has no runtime to resolve that, so before the fix it kept ONE
// successor as the entry and the rest fired a FALSE unreachable_node @1.0.
//
// The fix collects ALL START successors (from plain edges AND the conditional
// pathMap) and, when there are >=2, models `__start__` as a synthetic Control
// (typed "parallel", inert) entry node with one edge to each successor, so
// reachability flows to every parallel entry. Mirrors the wild
// linancn/tiangong-ai-langgraph-server learning_path_agent.ts (plain fan-out)
// and single_question_agent.ts (conditional-from-START fan-out) shapes.
import { StateGraph, START, END } from "@langchain/langgraph";

const graph = new StateGraph({ channels: {} })
  .addNode("getGraph", getGraph)
  .addNode("getRefs", getRefs)
  .addNode("getPortrait", getPortrait)
  .addNode("getKnowledge", getKnowledge)
  // Plain fan-out: START -> getGraph / getRefs / getPortrait (all parallel).
  .addEdge(START, "getGraph")
  .addEdge(START, "getRefs")
  .addEdge(START, "getPortrait")
  // Conditional-from-START contributes a fourth parallel entry (getKnowledge).
  .addConditionalEdges(START, routeStart, ["getKnowledge"])
  // The three plain entries converge, then exit.
  .addEdge("getGraph", "getKnowledge")
  .addEdge("getRefs", "getKnowledge")
  .addEdge("getPortrait", "getKnowledge")
  .addEdge("getKnowledge", END);

function getGraph(s: any) {
  return s;
}
function getRefs(s: any) {
  return s;
}
function getPortrait(s: any) {
  return s;
}
function getKnowledge(s: any) {
  return s;
}
function routeStart(s: any): "getKnowledge" {
  return "getKnowledge";
}

export const app = graph.compile();
