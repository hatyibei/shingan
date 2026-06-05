// Two routers reuse the SAME local binding name `next` with DIFFERENT values.
// Router-return identifier resolution must be scoped to each router's OWN body
// (codex #49/#51) — a file-global binding map keeps only the first `next` and
// would wire g2 to "answer" instead of "rewrite", falsely orphaning "rewrite".
import { StateGraph, START, END, MessagesAnnotation } from "@langchain/langgraph";

const routeA = (s: any) => { const next = "answer"; return next; };
const routeB = (s: any) => { const next = "rewrite"; return next; };

const g = new StateGraph(MessagesAnnotation)
  .addNode("g1", (s: any) => s)
  .addNode("answer", (s: any) => s)
  .addNode("g2", (s: any) => s)
  .addNode("rewrite", (s: any) => s)
  .addEdge(START, "g1")
  .addConditionalEdges("g1", routeA)
  .addEdge("answer", "g2")
  .addConditionalEdges("g2", routeB)
  .addEdge("rewrite", END)
  .compile();
