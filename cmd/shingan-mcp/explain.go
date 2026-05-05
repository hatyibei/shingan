package main

import "sort"

// ruleExplanations holds the human-readable description of every built-in
// Shingan rule. The keys must stay in sync with the switch in
// infrastructure/factory/analyzer.go — analyzer_test.go asserts the
// factory set, and main_test.go asserts parity with this map.
//
// Text style: short paragraph (what it detects + why it matters) followed by
// a "Severity" note and one concrete example. Keep it copy-pasteable into an
// editor tooltip.
var ruleExplanations = map[string]string{
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
}

// knownRuleNames returns the sorted list of rule identifiers, used to build
// a helpful error message when an unknown rule is requested.
func knownRuleNames() []string {
	names := make([]string, 0, len(ruleExplanations))
	for name := range ruleExplanations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
