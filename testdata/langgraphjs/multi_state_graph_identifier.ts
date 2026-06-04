// Multiple StateGraph graphs via a REUSED variable name with identifier-style
// `workflow.addNode(...)` (not a fluent chain) and mixing `addEdge(START,…)`
// with `setEntryPoint(…)`. The per-builder fluent disjoint-root check misses
// this style; the file-global StateGraph-root count catches it (codex review
// #49). Without the fix, graph 1's "A1" was falsely unreachable from graph 2's
// setEntryPoint entry "B1".
import { StateGraph, START, END, MessagesAnnotation } from "@langchain/langgraph";

let workflow = new StateGraph(MessagesAnnotation);
workflow.addNode("A1", (s: any) => s);
workflow.addEdge(START, "A1");
workflow.addEdge("A1", END);
const g1 = workflow.compile();

workflow = new StateGraph(MessagesAnnotation);
workflow.addNode("B1", (s: any) => s);
workflow.addNode("B2", (s: any) => s);
workflow.setEntryPoint("B1");
workflow.addEdge("B1", "B2");
workflow.addEdge("B2", END);
const g2 = workflow.compile();
