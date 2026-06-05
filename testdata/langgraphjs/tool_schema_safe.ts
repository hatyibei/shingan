// SECURITY fixture (safe / no-false-positive): a ToolNode aggregate whose
// tool's zod schema bounds every field. The shim converts the schema to
// `config.args_schema`, but `unbounded_tool_arg` must produce ZERO Warnings:
// every string has .max() (maxLength) and every array has .max() (maxItems).
//
//   START -> agent -> toolsNode -> END
import { StateGraph, START, END } from "@langchain/langgraph";
import { ToolNode } from "@langchain/langgraph/prebuilt";
import { tool } from "@langchain/core/tools";
import { z } from "zod";

const safeTool = tool(async (input: any) => "result", {
  name: "safe",
  description: "a well-bounded tool",
  schema: z.object({
    query: z.string().max(4000),
    tags: z.array(z.string().max(64)).max(16),
  }),
});

const tools = [safeTool];

const workflow = new StateGraph({ channels: {} });
workflow.addNode("agent", (state: any) => state);
workflow.addNode("toolsNode", new ToolNode(tools));
workflow.addEdge(START, "agent");
workflow.addEdge("agent", "toolsNode");
workflow.addEdge("toolsNode", END);

export const app = workflow.compile();
