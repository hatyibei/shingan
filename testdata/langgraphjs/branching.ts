// Branching LangGraph.js workflow: a classifier routes to one of two handlers
// via addConditionalEdges + an object pathMap, then both handlers exit to END.
// Exercises conditional edge extraction (pathMap key -> Edge.Condition) and
// has_exit_branch marking for the END-terminating handlers.
import { StateGraph, START, END } from "@langchain/langgraph";

const builder = new StateGraph({ channels: {} });
builder.addNode("classify", classifyNode);
builder.addNode("handleA", handleA);
builder.addNode("handleB", handleB);
builder.addEdge(START, "classify");
builder.addConditionalEdges("classify", router, {
  a: "handleA",
  b: "handleB",
});
builder.addEdge("handleA", END);
builder.addEdge("handleB", END);

function classifyNode(s: any) {
  return s;
}
function handleA(s: any) {
  return s;
}
function handleB(s: any) {
  return s;
}
function router(s: any): "a" | "b" {
  return s.kind;
}

export const app = builder.compile();
