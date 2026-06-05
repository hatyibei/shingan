// A path-map-LESS `addConditionalEdges(node, router)` whose router returns its
// destination via a ternary (`cond ? "answer" : "rewrite"`). The router-return
// harvester must recurse into the ConditionalExpression (both arms are dests) —
// and into a concise arrow body, which has no ReturnStatement — otherwise
// "answer"/"rewrite" are dropped and falsely flagged unreachable (dogfood:
// SaiUddisa/ecommerce-rag-langgraph). Regression guard.
import { StateGraph, START, END, MessagesAnnotation } from "@langchain/langgraph";

// concise-arrow ternary router
const routeByConfidence = (s: any) => (s.confidence >= 0.7 ? "answer" : "rewrite");

const g = new StateGraph(MessagesAnnotation)
  .addNode("grade", (s: any) => s)
  .addNode("answer", (s: any) => s)
  .addNode("rewrite", (s: any) => s)
  .addEdge(START, "grade")
  .addConditionalEdges("grade", routeByConfidence)
  .addEdge("answer", END)
  .addEdge("rewrite", "grade")
  .compile();
