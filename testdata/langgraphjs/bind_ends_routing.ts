// Class-method handlers bound via `.bind(this)` + the `addNode` 3rd-arg
// `{ ends: [...] }` static-routing option (wild shape: agentailor
// fullstack-langgraph-nextjs-agent src/lib/agent/builder.ts).
//
//   START -> agent
//   agent -> conditional(["tool_approval", END])     (array pathMap; END exit)
//   tool_approval -> Command({ goto: "tools" | "agent" })   (dynamic, via .bind)
//                 + addNode(..., { ends: ["tools", "agent"] })  (declarative)
//   tools -> agent
//
// Two gaps this fixture locks:
//  (1) The `tool_approval` handler is `this.approveToolCall.bind(this)` — a
//      `.bind()` CallExpression on a `this.method` PropertyAccess. resolveHandlerFn
//      must unwrap the `.bind` and resolve the class MethodDeclaration body so its
//      `new Command({goto: ...})` destinations are harvested.
//  (2) `addNode("tool_approval", handler, { ends: ["tools", "agent"] })` declares
//      the node's outgoing destinations up front; the 3rd-arg options object must
//      be parsed into outgoing edges.
//
// Either half restores tool_approval's edges; together "tools" is reachable
// (agent -> tool_approval -> tools) instead of firing a false unreachable_node,
// and the agent<->tool_approval<->tools ReAct cycle stays Warning (agent carries
// the structural END exit from the array pathMap), never Critical.
import { StateGraph, START, END, Command } from "@langchain/langgraph";

export class AgentBuilder {
  private toolNode: any;

  constructor(toolNode: any) {
    this.toolNode = toolNode;
  }

  private shouldApproveTool(state: any) {
    if (state.needsApproval) {
      return "tool_approval";
    }
    return END;
  }

  private approveToolCall(state: any) {
    if (state.approved) {
      return new Command({ goto: "tools" });
    }
    return new Command({ goto: "agent" });
  }

  private callModel(state: any) {
    return state;
  }

  build() {
    const stateGraph = new StateGraph({ channels: {} });
    stateGraph
      .addNode("agent", this.callModel.bind(this))
      .addNode("tools", this.toolNode)
      .addNode("tool_approval", this.approveToolCall.bind(this), {
        ends: ["tools", "agent"],
      })
      .addEdge(START, "agent")
      .addConditionalEdges("agent", this.shouldApproveTool.bind(this), [
        "tool_approval",
        END,
      ])
      .addEdge("tools", "agent");

    return stateGraph.compile();
  }
}
