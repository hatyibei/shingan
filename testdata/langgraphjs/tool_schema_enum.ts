import { StateGraph, MessagesAnnotation, START, END } from "@langchain/langgraph";
import { tool } from "@langchain/core/tools";
import { ToolNode } from "@langchain/langgraph/prebuilt";
import { z } from "zod";
const pick = tool(async ({ choice }) => choice, { name: "pick", description: "pick",
  schema: z.object({ choice: z.enum(["a","bb","ccc"]) }) });
const llm = async (s: any) => s;
const g = new StateGraph(MessagesAnnotation).addNode("llm", llm).addNode("tools", new ToolNode([pick]))
  .addEdge(START,"llm").addEdge("llm","tools").addEdge("tools",END).compile();
