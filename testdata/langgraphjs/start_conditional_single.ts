// Single conditional-from-START successor: `addConditionalEdges(START, router,
// ["getChapters"])` is the ONLY way START reaches the graph. Before the fix,
// handleConditional early-returned on a START source, so the lone successor
// never became the entry; the entry fell back to the first registered node and
// the real entry node fired a FALSE unreachable_node. Mirrors the wild
// linancn/tiangong-ai-langgraph-server kg_textbook_agent.ts shape.
//
// Behaviour lock: with exactly ONE START successor, the entry resolves to that
// node and NO synthetic `__start__` node is materialised (single-entry path is
// byte-identical to a plain `addEdge(START, x)`).
import { StateGraph, START, END } from "@langchain/langgraph";

const workflow = new StateGraph({ channels: {} })
  .addNode("getChapters", getChapters)
  .addNode("generateKG", generateKG)
  .addNode("getContents", getContents)
  .addConditionalEdges(START, routeStart, ["getChapters"])
  .addConditionalEdges("getChapters", routeChapters, ["getContents"])
  .addConditionalEdges("getContents", routeContents, ["generateKG"])
  .addEdge("generateKG", END);

function getChapters(s: any) {
  return s;
}
function generateKG(s: any) {
  return s;
}
function getContents(s: any) {
  return s;
}
function routeStart(s: any): "getChapters" {
  return "getChapters";
}
function routeChapters(s: any): "getContents" {
  return "getContents";
}
function routeContents(s: any): "generateKG" {
  return "generateKG";
}

export const graph = workflow.compile();
