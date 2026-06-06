// SECURITY fixture (vulnerable): a user-input source node reaches an LLM whose
// system prompt interpolates that input, creating a prompt-injection sink. The
// same config.system_prompt extraction that powers secret_in_prompt_template
// also enables prompt_injection_sink: classifySink fires on an LLM node whose
// system_prompt carries a `${…}` substitution, and isUserInputSource keys off a
// node named `user_*` / `*_input` / `query` / `request`.
//
//   START -> user_query -> agent -> END
//
// `user_query` is a user-input source (name pattern); `agent` is the LLM sink
// whose system_prompt embeds `${state.userText}` substitution. The path
// user_query -> agent makes prompt_injection_sink fire (Critical).
import { StateGraph, START, END } from "@langchain/langgraph";
import { ChatOpenAI } from "@langchain/openai";

const workflow = new StateGraph({ channels: {} });

workflow.addNode("user_query", (state: any) => {
  return { userText: state.input };
});

workflow.addNode("agent", async (state: any) => {
  const model = new ChatOpenAI({ apiKey: process.env.OPENAI_API_KEY });
  return model.invoke([
    {
      role: "system",
      content: `You are an assistant. Answer the question: ${state.userText}`,
    },
  ]);
});

workflow.addEdge(START, "user_query");
workflow.addEdge("user_query", "agent");
workflow.addEdge("agent", END);

export const app = workflow.compile();
