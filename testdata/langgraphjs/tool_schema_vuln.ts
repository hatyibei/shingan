// SECURITY fixture (vulnerable): a ToolNode aggregate whose tool's zod schema
// declares UNBOUNDED fields. The shim converts the tool() definitions' zod
// schemas into a merged JSON-schema `config.args_schema` on the aggregate tool
// node, so the Go `unbounded_tool_arg` rule can flag the missing bounds.
//
//   START -> agent -> toolsNode -> END
//
// `search` has a `query: z.string()` (no .max -> no maxLength) and a
// `tags: z.array(z.string())` (no .max -> no maxItems). Both are caller-/LLM-
// controllable blow-up vectors the rule must Warn on.
import { StateGraph, START, END } from "@langchain/langgraph";
import { ToolNode } from "@langchain/langgraph/prebuilt";
import { tool } from "@langchain/core/tools";
import { z } from "zod";

const searchTool = tool(async (input: any) => "result", {
  name: "search",
  description: "search the web",
  schema: z.object({
    query: z.string(),
    tags: z.array(z.string()),
  }),
});

const tools = [searchTool];

const workflow = new StateGraph({ channels: {} });
workflow.addNode("agent", (state: any) => state);
workflow.addNode("toolsNode", new ToolNode(tools));
workflow.addEdge(START, "agent");
workflow.addEdge("agent", "toolsNode");
workflow.addEdge("toolsNode", END);

export const app = workflow.compile();
