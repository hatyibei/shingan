// Agents-as-tools: an orchestrator agent uses child agents as callable tools via
// `child.asTool({...})` in its `tools` array. When the `.asTool()` receiver
// resolves to a DECLARED agent, the shim models it as a directed edge
// orchestrator -> child (same dest-must-be-declared gate as handoffs). Yields
// Orchestrator -> Spanish and Orchestrator -> French.
import { Agent } from "@openai/agents";

const spanishAgent = new Agent({
  name: "Spanish",
  instructions: "Translate the user's message to Spanish.",
});

const frenchAgent = new Agent({
  name: "French",
  instructions: "Translate the user's message to French.",
});

export const orchestrator = new Agent({
  name: "Orchestrator",
  instructions: "Use the translation tools to translate as requested.",
  tools: [
    spanishAgent.asTool({
      toolName: "translate_to_spanish",
      toolDescription: "Translate the user's message to Spanish.",
    }),
    frenchAgent.asTool({
      toolName: "translate_to_french",
      toolDescription: "Translate the user's message to French.",
    }),
  ],
});
