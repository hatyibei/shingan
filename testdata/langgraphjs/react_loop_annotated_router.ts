// React loop whose conditional router is a SEPARATELY-DECLARED function whose
// END exit is only visible through its return-type ANNOTATION (gap 3a):
//
//   function route(s): "tools" | typeof END
//
// The body returns `decide(state)` — an opaque call the shim cannot statically
// resolve — and there is NO pathMap on addConditionalEdges, so neither the body
// harvest nor a pathMap scan can see the END (or "tools") destinations. Only
// the annotation reveals them: its `"tools"` literal materialises the
// agent->tools edge and its `typeof END` member sets has_exit_branch on "agent"
// so the agent<->tools cycle downgrades cycle_detection to Warning.
import { StateGraph, START, END } from "@langchain/langgraph";

function route(s: any): "tools" | typeof END {
  return decide(s);
}

function decide(s: any): any {
  return s.next;
}

const workflow = new StateGraph({ channels: {} });
workflow.addNode("agent", callModel);
workflow.addNode("tools", toolNode);
workflow.addEdge(START, "agent");
workflow.addConditionalEdges("agent", route);
workflow.addEdge("tools", "agent");

function callModel(state: any) {
  return state;
}
function toolNode(state: any) {
  return state;
}

export const app = workflow.compile();
