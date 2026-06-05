// Multiple independent StateGraph definitions in ONE file, each compiled to its
// own app. Crucially they REUSE the variable name `workflow`, so the shim's
// per-varname builder MERGES all three graphs into one — collapsing three
// disjoint node sets under a single root with a single entry. Before the fix,
// graphs 2..N's nodes (everything not reachable from the first graph's START
// successor) fired FALSE unreachable_node @1.0. Mirrors the wild
// LinMoQC/Magic-Resume graphs.ts shape (three `const workflow = new
// StateGraph(...)` builders for research / analysis / rewrite).
//
// The fix detects the disjoint per-root node sets and flags entry_ambiguous
// (entry left empty), so domain/rules/reachability.go skips the graph rather
// than reporting graphs 2..N as unreachable. Mirrors the pydantic-graph
// multi-`Graph()` disjoint-node-set fix (#36).
import { StateGraph, START, END } from "@langchain/langgraph";

export const makeResearch = () => {
  const workflow = new StateGraph({ channels: {} })
    .addNode("preparer", preparer)
    .addNode("researcher", researcher)
    .addEdge(START, "preparer")
    .addEdge("preparer", "researcher")
    .addEdge("researcher", END);
  return workflow.compile();
};

export const makeAnalysis = () => {
  const workflow = new StateGraph({ channels: {} })
    .addNode("analyzer", analyzer)
    .addNode("combiner", combiner)
    .addEdge(START, "analyzer")
    .addEdge("analyzer", "combiner")
    .addEdge("combiner", END);
  return workflow.compile();
};

export const makeRewrite = () => {
  const workflow = new StateGraph({ channels: {} })
    .addNode("rewriter", rewriter)
    .addNode("finalizer", finalizer)
    .addEdge(START, "rewriter")
    .addEdge("rewriter", "finalizer")
    .addEdge("finalizer", END);
  return workflow.compile();
};

function preparer(s: any) {
  return s;
}
function researcher(s: any) {
  return s;
}
function analyzer(s: any) {
  return s;
}
function combiner(s: any) {
  return s;
}
function rewriter(s: any) {
  return s;
}
function finalizer(s: any) {
  return s;
}
