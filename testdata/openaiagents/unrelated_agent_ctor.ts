// `{ Agent }` is imported from @openai/agents, but `db.Agent` below is an
// UNRELATED library's constructor — it must NOT become an OpenAI Agents node
// (codex review #45). Only `triage` (a real `new Agent`) is a node.
import { Agent } from "@openai/agents";
import * as db from "some-orm";

const worker = new db.Agent({ name: "db_worker", handoffs: [] }); // unrelated → ignored
const triage = new Agent({ name: "Triage", handoffs: [] });
