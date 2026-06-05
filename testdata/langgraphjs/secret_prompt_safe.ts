// SECURITY fixture (safe / no-false-positive): an LLM node whose system prompt
// references the API key ONLY through `process.env.OPENAI_API_KEY` — there is
// NO hardcoded credential. The shim still lifts the static prompt text into
// config.system_prompt, but the Go `secret_in_prompt_template` rule must
// produce ZERO findings: it strips `process.env.X` / `${…}` placeholders before
// pattern-matching, and the remaining static text contains no secret pattern.
//
//   START -> agent -> END
import { StateGraph, START, END } from "@langchain/langgraph";
import { ChatOpenAI } from "@langchain/openai";
import { SystemMessage } from "@langchain/core/messages";

const workflow = new StateGraph({ channels: {} });

workflow.addNode("agent", async (state: any) => {
  const model = new ChatOpenAI({ apiKey: process.env.OPENAI_API_KEY });
  const systemPrompt =
    "You are a helpful assistant. Authenticate using the key from " +
    `the environment (${process.env.OPENAI_API_KEY}); never paste a raw key.`;
  return model.invoke([
    new SystemMessage(systemPrompt),
    { role: "user", content: state.input },
  ]);
});

workflow.addEdge(START, "agent");
workflow.addEdge("agent", END);

export const app = workflow.compile();
