// Namespace import: `oa.Agent` / `oa.Agent.create` / `oa.handoff` must be
// recognized as @openai/agents constructors/helpers (codex review #45).
import * as oa from "@openai/agents";

const specialist = new oa.Agent({ name: "Specialist", handoffs: [] });
const triage = oa.Agent.create({ name: "Triage", handoffs: [specialist, oa.handoff(specialist)] });
