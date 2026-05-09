package application

import "sort"

// ruleExplanations holds the human-readable description of every built-in
// Shingan rule. The keys must stay in sync with the switch in
// infrastructure/factory/analyzer.go — analyzer_test.go asserts the
// factory set, and main_test.go asserts parity with this map.
//
// Text style: short paragraph (what it detects + why it matters) followed by
// a "Severity" note and one concrete example. Keep it copy-pasteable into an
// editor tooltip.
var RuleExplanations = map[string]string{
	"cycle_detection": `cycle_detection — detects directed cycles in the workflow graph via DFS back-edge tracking.
Why it matters: A cycle with no bounded loop guard is a runaway cost / infinite-retry risk.
Severity: Critical (Confidence 1.0 — deterministic).
Example: node A → B → A with no LoopAgent wrapper and no max_iterations flag.`,

	"unreachable_node": `unreachable_node — reports nodes not reachable from entry_node_id via BFS.
Why it matters: Dead nodes are almost always stale code paths, renamed but not removed, or an entry-node typo.
Severity: Warning (Confidence 1.0 — deterministic).
Example: You wire "classify" → "respond" but forget to connect "classify" from entry_node, so the whole subgraph is orphaned.`,

	"error_handler_checker": `error_handler_checker — checks that tool/LLM nodes have an error path within 2 hops.
Why it matters: Unhandled errors either crash the run or, worse, silently return empty answers that downstream nodes treat as valid.
Severity: Warning (Confidence 0.8 — heuristic, 2-hop window).
Example: An HTTP-calling Tool node with no outgoing edge labelled "error"/"fail"/"retry".`,

	"cost_estimation": `cost_estimation — flags LLM nodes using high-cost models without cost-awareness signals.
Why it matters: Using gpt-4o everywhere silently burns money when half of the nodes would work on mini tiers.
Severity: Info (Confidence 0.7 — pricing tiers drift).
Example: An "extract_entity" classification node configured with gpt-4o where gpt-4o-mini would match quality.`,

	"redundant_llm_call": `redundant_llm_call — detects multiple LLM nodes sharing the same prompt_template in one graph.
Why it matters: Identical prompts usually mean you forgot to refactor, and you pay N× for what should be one cached call.
Severity: Warning (Confidence 0.9 — exact prompt match).
Example: Both "summarize_a" and "summarize_b" with prompt_template="Summarize: {text}".`,

	"loop_guard": `loop_guard — flags LoopAgent / control nodes that lack Config["max_iterations"].
Why it matters: A Loop with no bound will happily spin until the provider rate-limits or the bill explodes.
Severity: Critical (Confidence 1.0 — Config key presence is deterministic).
Example: {"type":"loop","config":{}} — missing max_iterations.`,

	"pii_leak_scanner": `pii_leak_scanner — traces paths from PII sources (RAG nodes, nodes with has_pii=true) to external sinks (tool.category in {api, mcp, browser}) without a Human approval gate.
Why it matters: This is the easiest way to ship a GDPR / CCPA leak without noticing.
Severity: Warning for explicit PII (Confidence 0.6), Info for name-hint matches (Confidence 0.3).
Example: "rag_lookup" → "send_email" with no human-in-the-loop approval node between.`,

	"secret_exposure_scanner": `secret_exposure_scanner — matches prompts, configs, and tool arguments against regex for API keys, JWTs, cloud credentials, and passwords.
Why it matters: Secrets committed to a graph definition are secrets leaked — they end up in logs, checkpoints, and third-party inference traces.
Severity: Critical (Confidence 0.95) for AWS/GCP/JWT patterns; Info (Confidence 0.5) for generic "password=" fragments.
Example: A Tool node's Config contains api_key: "sk-live-...".`,

	"max_parallel_branches": `max_parallel_branches — counts fan-out from each node.
Why it matters: 100+ parallel LLM calls from a single step both hits rate limits and generates a coordinated cost spike.
Severity: Critical at ≥100 (Confidence 1.0), Warning at ≥20 (0.9), Info at ≥10 (0.7). Nodes with Config["max_concurrency"] set are skipped.
Example: A ParallelAgent fanning out to 200 sub-agents without max_concurrency.`,

	"deprecated_model": `deprecated_model — catches LLM nodes configured with shutdown or soon-to-be-deprecated models.
Why it matters: Deprecated models get shut down; your workflow silently starts 400-ing the day it happens.
Severity: Critical for shutdown models (Confidence 1.0), Warning for deprecated-soon (Confidence 0.9). Covers 20 models across OpenAI / Anthropic / Google.
Example: A node using gpt-3.5-turbo-0301 (shutdown) or claude-2 (deprecated).`,

	"temperature_misuse": `temperature_misuse — flags LLM nodes that combine temperature > 0 with a deterministic task signature (structured output / extraction / classification / code generation).
Why it matters: Schema-bound or label-bound tasks need a deterministic decode. High temperature creates output drift between runs, breaks JSON parsing, and inflates eval variance.
Severity: Warning when structured_output=true or response_format="json_object" alongside temp>0 (Confidence 0.9, exact_static_match). Warning for classification with temp>0.3 or code_generation with temp>0 (Confidence 0.7, heuristic_pattern). Info for extraction tasks with temp>0 (Confidence 0.5, heuristic_pattern). Falls back to node.Name keyword scanning when Config["task"] is absent.
Example: An LLM node with model="gpt-4o-mini", structured_output=true, temperature=0.7 → Warning.`,

	"model_card_mismatch": `model_card_mismatch — detects LLM nodes whose declared model name disagrees with the configured base_url or provider.
Why it matters: A gpt-* model wired to api.anthropic.com will fail at runtime with a hard 4xx; the static check catches it before deploy.
Severity: Critical when a known prefix (gpt-*, claude-*, gemini-*, o1-*, text-bison*, chat-bison*) disagrees with provider/base_url (Confidence 1.0, exact_static_match). Info when the model prefix is unknown but a provider is set (Confidence 0.4, heuristic_pattern, surfaced so the table can be extended). No finding when only base_url is set without provider for unknown prefixes, or when provider matches the model prefix even with a custom base_url (legitimate proxy).
Example: model="gpt-4o" + base_url="https://api.anthropic.com/v1" → Critical.`,

	"prompt_injection_sink": `prompt_injection_sink — traces paths from user-input nodes (Config["source"]=="user_input" or names matching user_*/query/request/*_input) to LLM nodes whose Config carries a prompt-template field (system_prompt, prompt_template, user_message_template, instruction).
Why it matters: When attacker-controllable text is concatenated into a system prompt, an injected instruction can override the agent's policies — credential exfiltration, jailbreak, tool abuse. Static graph analysis catches the structural sink before runtime sanitization can save you.
Severity: Critical for system_prompt with {{var}}/${var}/{var} substitution (Confidence 0.9); Warning for system_prompt without substitution (Confidence 0.7); Info for non-system templates with substitution (Confidence 0.5). All findings carry ConfidenceReason=heuristic_pattern.
Example: A "user_query" tool node feeding into an LLM whose Config["system_prompt"] = "You are an assistant. Context: {{user_query}}." → Critical. Mitigation: keep user content in a separate role: user message and reserve the system prompt for trusted strings.`,

	"eval_missing": `eval_missing — traces paths from any LLM node to a code-execution Tool sink (Config["category"] ∈ {code_execution, code_eval}, Config["tool"] ∈ {eval, exec, code_interpreter, python_runner, shell}, or names matching eval/exec/code_runner/python_runner/shell/bash patterns).
Why it matters: When raw LLM output is fed into eval()/exec()/Function()/code_interpreter, an injected payload becomes arbitrary code execution. Validation (a Condition node) helps but is rarely complete; Human-in-the-loop approval is the only path the rule treats as safe.
Severity: Critical when no validation gate exists between the LLM and the sink (Confidence 0.9, ConfidenceReason heuristic_pattern). Warning when a Condition node sits on the path (Confidence 0.6, downgraded but not silenced). Paths through a Human approver are skipped entirely.
Example: An LLM node feeding directly into a Tool with Config["category"] = "code_execution" → Critical. Mitigation: validate / parse / sandbox the output, switch to a structured tool-call schema, or insert a Human approval gate.`,

	"dynamic_node_construction": `dynamic_node_construction — scans a curated subset of Node.Config keys (body, fn, handler, callback, code, factory, builder) for runtime code-construction patterns: eval(/exec(/Function(/compile(/__import__(/getattr(/setattr(.
Why it matters: Dynamic code construction lets workflow authors generate logic at runtime, defeating static analysis and (when the input is attacker-controllable) opening an RCE attack surface. The rule complements eval_missing — that one looks at structural reachability between an LLM and a code-execution Tool; this one inspects the literal string content of Config values.
Severity: Critical for eval(/exec(/Function( (Confidence 0.95, exact_static_match). Warning for compile(/__import__( (Confidence 0.85, exact_static_match). Info for getattr(/setattr( (Confidence 0.6, heuristic_pattern). Pure placeholder values like "${EVAL_FN}" are skipped; mixed values like "eval(${PAYLOAD})" still fire because eval( survives placeholder removal.
Example: A Tool node with Config["body"] = "lambda x: eval(x)" → Critical. Mitigation: refactor to explicit dispatch tables (commands = {"sum": handler_sum, ...}) or use a sandboxed evaluator with allowlist.`,

	"retry_storm": `retry_storm — flags Tool nodes whose retry configuration combined with the surrounding parallelism produces a high blast radius against an upstream API.
Why it matters: A retry-loop with no exponential backoff or shared rate limit can fan a single transient outage into a coordinated stampede that takes the upstream provider out, exhausts your token budget, or trips IP-based rate limits.
Algorithm: Source = Tool with Config["retries"|"max_retries"|"retry_count"] >= 3. Parallelism = max(fan-in count, source's max_concurrency, upstream Loop's max_iterations, upstream node's max_concurrency). blast = retries × parallelism. Severity by blast: >=100 Critical (Confidence 0.9, exact_static_match), >=30 Warning (Confidence 0.7, heuristic_pattern), >=10 Info (Confidence 0.5, heuristic_pattern).
Example: An api Tool with retries=5 and max_concurrency=20 → blast 100 → Critical. Mitigation: add exponential backoff, a circuit breaker, or a shared rate limiter across the parallel orchestrator.`,

	"circular_dep_agents": `circular_dep_agents — detects multi-agent workflows in which one agent can delegate to another that delegates back, creating a delegation cycle.
Why it matters: Without an explicit max_handoffs budget or an orchestrator pattern, two or more agents that can hand control to each other risk infinite delegation. cycle_detection catches this structurally (Critical), but circular_dep_agents adds an agent-aware Warning so the message and suggestion can speak to "agent A → agent B → agent A" specifically.
Algorithm: agents are identified by Config["agent_role"] (LangGraph / ADK convention) or Config["sub_agents"] non-empty. DFS forward from each agent looking for a path that returns to it; the cycle's distinct-agent count drives the Severity / Confidence pair: 2-agent cycle → Warning 0.85 exact_static_match, 3+ agent cycle → Warning 0.75 exact_static_match, self-reference → Info 0.6 heuristic_pattern.
Example: planner_agent → worker_agent → planner_agent → Warning. Mitigation: switch to an orchestrator pattern or set explicit max_handoffs.`,

	"unbounded_tool_arg": `unbounded_tool_arg — flags Tool nodes whose JSON-schema-shaped args_schema / parameters / input_schema contains fields with no upper bound (string maxLength, array maxItems, number maximum).
Why it matters: An LLM (or attacker) can send a monster payload through an unbounded Tool argument, blowing up token usage, hitting provider rate limits, or OOM-ing the tool runtime.
Severity: Warning when string maxLength is missing (Confidence 0.7) or array maxItems is missing (Confidence 0.7); Info when string maxLength exceeds 100K (Confidence 0.5) or number maximum is missing (Confidence 0.4). All findings carry ConfidenceReason=heuristic_pattern. Findings are capped at 5 per node.
Example: A Tool node with Config["args_schema"]={"type":"object","properties":{"query":{"type":"string"}}} → Warning. Mitigation: add maxLength (4000-32000), maxItems, or maximum.`,

	"secret_in_prompt_template": `secret_in_prompt_template — narrow rule that flags hardcoded credentials inside LLM prompt templates: system_prompt / prompt_template / user_message_template / instruction.
Why it matters: API keys pasted into prompt-engineering iterations leak through every channel a workflow definition touches — version control, logs, exported runs, third-party inference traces.
Severity: Critical for AWS access keys, OpenAI/Anthropic API keys, GitHub tokens, and PEM blocks (Confidence 0.95, exact_static_match). Warning for JWTs (Confidence 0.7, heuristic_pattern). Environment-variable substitutions are stripped before matching.
Example: Config["system_prompt"]="You are X. Use sk-abc123..." → Critical. Mitigation: switch to ${API_KEY} env-var substitution and rotate the leaked credential.`,

	"missing_eval_dataset": `missing_eval_dataset — flags workflow graphs that declare a production / staging deployment signal anywhere in the graph but carry no eval dataset / benchmark reference.
Why it matters: Production AI agents need regression eval to catch model upgrades that change behavior. Without an eval_dataset / benchmark reference, model drift goes undetected until a customer complains.
Algorithm: OnGraph aggregation (one finding per graph). Trigger when ANY node Config has env=prod|staging or deployment=true and NO node carries eval_dataset / test_set / benchmark.
Severity: Warning (Confidence 0.7, heuristic_pattern). Mitigation: add Config["eval_dataset"] pointing to a versioned test set on the entry or any node.`,

	"tool_description_missing": `tool_description_missing — flags Tool nodes whose Config has no usable description / doc / summary / help / purpose field (≥10 chars).
Why it matters: LLM agents pick which tool to call from the description text alone. A Tool with no description means the model is guessing, which leads to wrong-tool selection, hallucinated arguments, or unnecessary API calls. Even short identifiers like "search" or "send_email" are too ambiguous in tool-use settings — what does it search? send to whom?
Severity: Info (Confidence 0.6, heuristic_pattern). Trigger / webhook nodes (Config["category"] == "trigger") are exempt because they're not LLM-facing. Tools whose Name reads as a natural-language sentence (≥3 space-separated words) also pass since the Name itself describes the tool.
Example: A Tool with Config["description"]="" or with no description key, named "search" — Info finding. Mitigation: Config["description"] = "Search the public web index and return top-5 results as JSON.". 2-3 sentences is the sweet spot for tool-use selection accuracy.`,

	"human_gate_missing": `human_gate_missing — flags production-deployed graphs that perform sensitive actions (API writes, code execution, payments, data egress, browser automation) without any Human-type approval node in the workflow.
Why it matters: AI agents make irreversible side effects (sending money, deleting data, posting publicly). Production graphs without a human-in-the-loop gate fail SOC 2 / ISO 42001 governance reviews and are an incident waiting to happen — the model executes the wrong thing in the wrong context with no chance to intervene. Complements pii_leak_scanner (which traces specific source→sink paths) by enforcing graph-wide governance posture.
Algorithm: OnGraph aggregation (one finding per graph). Trigger when ALL of: (1) ANY node Config has env=prod|staging|production OR deployment=true, AND (2) ANY Tool node has Config["category"] in {code_execution, api, mcp, browser, trigger} OR a name matching send|post|delete|transfer|payment|email|webhook|execute|deploy|publish|fire, AND (3) NO node has Type==NodeTypeHuman.
Severity: Warning (Confidence 0.6, heuristic_pattern — naming heuristic for the deploy + sensitive signals). Mitigation: insert a single graph-wide Human approval node before sensitive Tool calls; finer scoping handled by pii_leak_scanner / eval_missing.`,
}

// KnownRuleNames returns the sorted list of rule identifiers, used to build
// a helpful error message when an unknown rule is requested.
func KnownRuleNames() []string {
	names := make([]string, 0, len(RuleExplanations))
	for name := range RuleExplanations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ExplainRule returns the explanation text for the given rule name, or
// "" + false if the rule is unknown to the current Shingan release.
func ExplainRule(ruleName string) (string, bool) {
	text, ok := RuleExplanations[ruleName]
	return text, ok
}
