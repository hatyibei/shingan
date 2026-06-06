// SECURITY fixture (vulnerable): an LLM node whose system prompt contains a
// HARDCODED credential. The shim lifts the static prompt text out of the
// handler body into config.system_prompt, so the Go `secret_in_prompt_template`
// rule can scan it and FIRE.
//
//   START -> agent -> END
//
// The handler builds a chat model (LLM classification) and invokes it with a
// `{ role: "system", content: "…" }` message whose content embeds an AWS
// access key. NOTE on literals: the rule's openai pattern is `sk-[A-Za-z0-9]{20,}`
// — a hyphen STOPS the run, so the canonical `sk-proj-…` form does NOT match.
// We therefore use a literal that provably matches a rule pattern:
//   * AKIAIOSFODNN7EXAMPLE   -> aws_access_key (`AKIA[0-9A-Z]{16}`)
//   * sk-abcdefABCDEF0123456789xyz -> openai_api_key (hyphen-free, >=20 alnum)
import { StateGraph, START, END } from "@langchain/langgraph";
import { ChatOpenAI } from "@langchain/openai";

const workflow = new StateGraph({ channels: {} });

workflow.addNode("agent", async (state: any) => {
  const model = new ChatOpenAI({ model: "gpt-4o" });
  return model.invoke([
    {
      role: "system",
      content:
        "You are a deployment assistant. Use AWS key AKIAIOSFODNN7EXAMPLE and " +
        "fallback token sk-abcdefABCDEF0123456789xyz to reach the cluster.",
    },
    { role: "user", content: state.input },
  ]);
});

workflow.addEdge(START, "agent");
workflow.addEdge("agent", END);

export const app = workflow.compile();
