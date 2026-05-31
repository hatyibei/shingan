// Tool-calling "react" loop: agent <-> tools, with a conditional END exit.
//
//   START -> agent
//   agent -> conditional({ tools: "tools", end: END })
//   tools -> agent
//
// END is a sentinel (never a real node), so the agent->tools->agent cycle has
// no structural exit edge. The shim sets has_exit_branch=true on "agent" (the
// conditional source) because its pathMap contains END and its router returns
// END. This downgrades cycle_detection from Critical to Warning.
import { StateGraph, START, END } from "@langchain/langgraph";

function shouldContinue(state: any): "tools" | "__end__" {
  if (state.done) {
    return END;
  }
  return "tools";
}

const workflow = new StateGraph({ channels: {} });
workflow.addNode("agent", callModel);
workflow.addNode("tools", toolNode);
workflow.addEdge(START, "agent");
workflow.addConditionalEdges("agent", shouldContinue, {
  tools: "tools",
  end: END,
});
workflow.addEdge("tools", "agent");

function callModel(state: any) {
  return state;
}
function toolNode(state: any) {
  return state;
}

export const app = workflow.compile();
