import { StateGraph, MessagesAnnotation, START, END } from "@langchain/langgraph";
import { ChatOpenAI } from "@langchain/openai";
const model = new ChatOpenAI({});
const nodeA = async (s: any) => { const systemPrompt = "key AKIAIOSFODNN7EXAMPLE here"; return model.invoke(systemPrompt); };
const nodeB = async (s: any) => { const systemPrompt = "totally safe prompt no secret"; return model.invoke(systemPrompt); };
const g = new StateGraph(MessagesAnnotation).addNode("a", nodeA).addNode("b", nodeB)
  .addEdge(START,"a").addEdge("a","b").addEdge("b",END).compile();
