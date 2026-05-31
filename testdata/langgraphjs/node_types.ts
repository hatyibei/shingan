// Node-type classification (gap 4). Each handler is inspected for its construct
// / body so the type is NOT decided by the node name alone (the pre-change
// behavior). Node names are deliberately chosen to NOT match the tool name
// regex (tool|retriev|search|fetch|browser), so every "tool" classification
// here comes from construct/body inspection, not the name:
//
//   "step"   -> inline `new ToolNode(...)` handler            => "tool"
//   "exec"   -> handler var bound to `new ToolNode(...)`      => "tool" (varInits)
//   "agent"  -> body constructs/invokes a chat model AND      => "llm"
//               binds tools (model signal WINS the tie)
//   "runner" -> body references the `tools` array only        => "tool"
//   "plain"  -> opaque passthrough                            => "llm" (default)
import { StateGraph, START, END } from "@langchain/langgraph";
import { ChatOpenAI } from "@langchain/openai";
import { ToolNode } from "@langchain/langgraph/prebuilt";

const tools = [searchTool, calcTool];
const executorVar = new ToolNode(tools);

const graph = new StateGraph({ channels: {} });
graph.addNode("step", new ToolNode(tools));
graph.addNode("exec", executorVar);
graph.addNode("agent", agentNode);
graph.addNode("runner", runStep);
graph.addNode("plain", plainNode);
graph.addEdge(START, "step");
graph.addEdge("step", "agent");
graph.addEdge("agent", "runner");
graph.addEdge("runner", "exec");
graph.addEdge("exec", "plain");
graph.addEdge("plain", END);

// Classic agent node: builds a chat model and binds tools to it. BOTH a model
// signal and a tools reference are present; the model signal must win so this
// stays "llm" (a wrong "tool" would be worse than the safe default).
function agentNode(state: any) {
  const model = new ChatOpenAI({ model: "gpt-4o" });
  return { messages: [model.bindTools(tools).invoke(state.messages)] };
}

// Tool-execution body with NO model signal: references the tools array only.
function runStep(state: any) {
  return { result: tools.map((t: any) => t.call(state)) };
}

function plainNode(state: any) {
  return state;
}

const searchTool: any = {};
const calcTool: any = {};

export const app = graph.compile();
