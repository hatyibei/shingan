// Package testutil provides helpers for constructing WorkflowGraph instances
// in tests and developer tooling without requiring a real framework parser.
//
// This file contains pattern-specific graph generators that produce intentional
// structural patterns — both clean and deliberately buggy — for use in testing,
// benchmarking, and sample generation via the shingan-gen CLI.
package testutil

import (
	"fmt"
	"math/rand"

	"github.com/hatyibei/shingan/domain"
)

// GenerateCleanGraph generates a structurally correct WorkflowGraph with n nodes.
//
// Properties:
//   - Linear sequential chain: entry → LLM → ... → Output
//   - All Tool nodes have conditional outgoing edges (error handling present)
//   - No cycles, no unreachable nodes, no duplicate LLM (model, prompt_template)
//   - Loop nodes always have max_iterations set (< 100)
//
// Expected findings: none (0 findings from all 7 rules).
func GenerateCleanGraph(n int, seed int64) *domain.WorkflowGraph {
	if n <= 0 {
		n = 3
	}
	rng := rand.New(rand.NewSource(seed))
	_ = rng

	nodes := make(map[string]*domain.Node, n+2)
	edges := make([]domain.Edge, 0, n+1)

	// Entry LLM node
	entryID := "clean_entry"
	nodes[entryID] = &domain.Node{
		ID:   entryID,
		Name: "entry_llm",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-4o-mini",
			"prompt_template": "analyze_request",
		},
	}

	prevID := entryID

	// Middle LLM nodes (each with a unique prompt_template to avoid redundant_llm_call)
	for i := 0; i < n-1; i++ {
		nodeID := fmt.Sprintf("clean_step_%02d", i)
		model := "claude-3-haiku" // mid-tier, not high-cost
		if i%3 == 0 {
			model = "gpt-4o-mini" // low-tier
		}
		nodes[nodeID] = &domain.Node{
			ID:   nodeID,
			Name: fmt.Sprintf("step_%02d", i),
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model":           model,
				"prompt_template": fmt.Sprintf("step_template_%02d", i),
			},
		}
		edges = append(edges, domain.Edge{From: prevID, To: nodeID})
		prevID = nodeID
	}

	// Output node
	outputID := "clean_output"
	nodes[outputID] = &domain.Node{
		ID:     outputID,
		Name:   "final_output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}
	edges = append(edges, domain.Edge{From: prevID, To: outputID})

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: entryID,
	}
}

// GenerateInfiniteLoopGraph generates a WorkflowGraph that triggers the
// loop_guard and cycle_detection rules.
//
// Structure:
//   - entry → loop_node (NodeTypeLoop, no max_iterations) → worker_a → worker_b → loop_node
//
// Expected findings:
//   - loop_guard: Critical (loop_node has no max_iterations)
//   - cycle_detection: Critical (Loop node in cycle, max_iterations not set)
func GenerateInfiniteLoopGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // seed unused for this deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// LoopAgent without max_iterations — triggers loop_guard (Critical)
	nodes["loop_node"] = &domain.Node{
		ID:     "loop_node",
		Name:   "unbounded_loop",
		Type:   domain.NodeTypeLoop,
		Config: map[string]any{}, // no max_iterations — deliberate bug
	}

	nodes["worker_a"] = &domain.Node{
		ID:   "worker_a",
		Name: "worker_a",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model": "gpt-4o-mini",
		},
	}

	nodes["worker_b"] = &domain.Node{
		ID:   "worker_b",
		Name: "worker_b",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model": "gpt-4o-mini",
		},
	}

	// Loop: loop_node → worker_a → worker_b → loop_node (back edge)
	edges = append(edges,
		domain.Edge{From: "loop_node", To: "worker_a"},
		domain.Edge{From: "worker_a", To: "worker_b"},
		domain.Edge{From: "worker_b", To: "loop_node"}, // back edge = cycle
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "loop_node",
	}
}

// GenerateUnreachableGraph generates a WorkflowGraph with detached (unreachable) nodes.
//
// Structure:
//   - entry → llm_chain → output  (reachable)
//   - dangling_llm, dangling_tool  (unreachable — no edges connecting them)
//
// Expected findings:
//   - unreachable_node: Warning (dangling_llm, dangling_tool are LLM/Tool type)
func GenerateUnreachableGraph(n int, seed int64) *domain.WorkflowGraph {
	if n < 5 {
		n = 5
	}
	rng := rand.New(rand.NewSource(seed))
	_ = rng

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// Reachable chain: entry → steps → output
	entryID := "entry_node"
	nodes[entryID] = &domain.Node{
		ID:   entryID,
		Name: "entry",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-4o-mini",
			"prompt_template": "entry_template",
		},
	}

	// Reachable nodes (n-2 nodes, leaving 2 for unreachable)
	reachableCount := n - 2
	if reachableCount < 2 {
		reachableCount = 2
	}
	prevID := entryID
	for i := 0; i < reachableCount-1; i++ {
		nodeID := fmt.Sprintf("reachable_%02d", i)
		nodes[nodeID] = &domain.Node{
			ID:   nodeID,
			Name: fmt.Sprintf("step_%02d", i),
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model":           "claude-3-haiku",
				"prompt_template": fmt.Sprintf("template_%02d", i),
			},
		}
		edges = append(edges, domain.Edge{From: prevID, To: nodeID})
		prevID = nodeID
	}

	outputID := "reachable_output"
	nodes[outputID] = &domain.Node{
		ID:     outputID,
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}
	edges = append(edges, domain.Edge{From: prevID, To: outputID})

	// Unreachable nodes (detached — no edges) — triggers unreachable_node Warning
	nodes["dangling_llm"] = &domain.Node{
		ID:   "dangling_llm",
		Name: "dangling_llm",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model": "gpt-4o-mini",
		},
	}
	nodes["dangling_tool"] = &domain.Node{
		ID:   "dangling_tool",
		Name: "dangling_tool",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "api",
		},
	}

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: entryID,
	}
}

// GeneratePIILeakGraph generates a WorkflowGraph that triggers the pii_leak_scanner rule.
//
// Structure:
//   - entry → rag_tool (category=rag) → llm_node → api_sink (category=api, no Human gate)
//
// Expected findings:
//   - pii_leak_scanner: Warning (RAG tool → external API without Human approval gate)
func GeneratePIILeakGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// RAG source node — isRAGSource() returns true for category=="rag"
	nodes["rag_tool"] = &domain.Node{
		ID:   "rag_tool",
		Name: "user_data_rag",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "rag",
		},
	}

	// LLM node that processes RAG output
	nodes["llm_processor"] = &domain.Node{
		ID:   "llm_processor",
		Name: "llm_processor",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model": "gpt-4o-mini",
		},
	}

	// External API sink — isExternalSink() returns true for category=="api"
	// No Human gate between rag_tool and api_sink → triggers pii_leak_scanner
	nodes["api_sink"] = &domain.Node{
		ID:   "api_sink",
		Name: "external_api",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "api",
		},
	}

	// Output node
	nodes["output"] = &domain.Node{
		ID:     "output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	// Direct path: rag_tool → llm_processor → api_sink (no Human gate)
	edges = append(edges,
		domain.Edge{From: "rag_tool", To: "llm_processor"},
		domain.Edge{From: "llm_processor", To: "api_sink"},
		domain.Edge{From: "api_sink", To: "output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "rag_tool",
	}
}

// GenerateCycleGraph generates a WorkflowGraph with a raw cycle not wrapped
// in a Loop/LoopAgent node — a graph definition error.
//
// Structure:
//   - entry → llm_a → llm_b → llm_c → llm_a (back edge, no parent Loop node)
//   - output node reachable from llm_c via a separate conditional edge
//
// Expected findings:
//   - cycle_detection: Critical (non-Loop node forms a cycle with no parent Loop guard)
func GenerateCycleGraph(n int, seed int64) *domain.WorkflowGraph {
	if n < 3 {
		n = 3
	}
	_ = seed // deterministic structure

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// Create n LLM nodes in a cycle
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("cycle_llm_%02d", i)
		ids[i] = id
		nodes[id] = &domain.Node{
			ID:   id,
			Name: fmt.Sprintf("cycle_llm_%02d", i),
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model": "gpt-4o-mini",
			},
		}
	}

	// Linear chain forward
	for i := 0; i < n-1; i++ {
		edges = append(edges, domain.Edge{From: ids[i], To: ids[i+1]})
	}
	// Back edge from last to first — creates a raw cycle
	edges = append(edges, domain.Edge{From: ids[n-1], To: ids[0]})

	// Output node (also reachable from the last node via conditional edge)
	nodes["cycle_output"] = &domain.Node{
		ID:     "cycle_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}
	edges = append(edges, domain.Edge{From: ids[n-1], To: "cycle_output", Condition: "done"})

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: ids[0],
	}
}

// GenerateSecretExposureGraph generates a WorkflowGraph that triggers the
// secret_exposure_scanner rule with a Critical finding.
//
// Structure:
//   - entry LlmAgent node whose Config["prompt"] contains a hardcoded OpenAI API key
//
// Expected findings:
//   - secret_exposure_scanner: Critical (hardcoded sk- key in prompt field)
func GenerateSecretExposureGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// LLM node with a hardcoded secret in its prompt field.
	// The key "sk-abcdefghijklmnopqrstuvwxyz1234567890" matches the openai_api_key pattern
	// (sk-[A-Za-z0-9]{20,}) and will produce a Critical finding.
	nodes["secret_agent"] = &domain.Node{
		ID:   "secret_agent",
		Name: "secret_llm_agent",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":  "gpt-4o-mini",
			"prompt": "Use key sk-abcdefghijklmnopqrstuvwxyz1234567890 for auth",
		},
	}

	// Output node
	nodes["secret_output"] = &domain.Node{
		ID:     "secret_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "secret_agent", To: "secret_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "secret_agent",
	}
}

// GenerateDeprecatedModelGraph generates a WorkflowGraph that triggers the
// deprecated_model rule with a Critical finding.
//
// Structure:
//   - entry LLM node whose Config["model"] is "gpt-3.5-turbo-0613" (shutdown model)
//
// Expected findings:
//   - deprecated_model: Critical (gpt-3.5-turbo-0613 was shut down on 2024-09-13)
func GenerateDeprecatedModelGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// LLM node using a shutdown model — triggers deprecated_model Critical.
	nodes["deprecated_entry"] = &domain.Node{
		ID:   "deprecated_entry",
		Name: "deprecated_llm_agent",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-3.5-turbo-0613",
			"prompt_template": "classify_request",
		},
	}

	// Output node
	nodes["deprecated_output"] = &domain.Node{
		ID:     "deprecated_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "deprecated_entry", To: "deprecated_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "deprecated_entry",
	}
}

// GenerateBuggyGraph generates a WorkflowGraph that fires all 7 analysis rules.
//
// Rules triggered:
//   1. cycle_detection: Critical — Loop node in cycle, no max_iterations
//   2. loop_guard: Critical — Loop node without max_iterations
//   3. unreachable_node: Warning — dangling LLM node
//   4. error_handler_checker: Warning — Tool node (api) with only unconditional outgoing edges
//   5. cost_estimation: Warning — high-cost LLM (gpt-4o) inside a loop
//   6. redundant_llm_call: Warning — two LLM nodes with identical (model, prompt_template)
//   7. pii_leak_scanner: Warning — RAG tool → external API without Human gate
func GenerateBuggyGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// === Subgraph 1: Infinite loop (triggers loop_guard + cycle_detection) ===
	// Also triggers cost_estimation: gpt-4o LLM node is inside the loop

	// LoopAgent without max_iterations
	nodes["buggy_loop"] = &domain.Node{
		ID:     "buggy_loop",
		Name:   "buggy_loop",
		Type:   domain.NodeTypeLoop,
		Config: map[string]any{}, // no max_iterations — triggers loop_guard Critical
	}

	// High-cost LLM inside the loop — triggers cost_estimation Warning
	nodes["expensive_llm"] = &domain.Node{
		ID:   "expensive_llm",
		Name: "expensive_llm",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-4o", // high-cost model — triggers cost_estimation
			"prompt_template": "shared_template",
		},
	}

	// Duplicate LLM — same model + prompt_template as expensive_llm — triggers redundant_llm_call
	nodes["duplicate_llm"] = &domain.Node{
		ID:   "duplicate_llm",
		Name: "duplicate_llm",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "shared_template", // same as expensive_llm — triggers redundant_llm_call
		},
	}

	// Tool node with unconditional outgoing edges — triggers error_handler_checker Warning
	nodes["api_tool"] = &domain.Node{
		ID:   "api_tool",
		Name: "api_tool",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "api", // Warning severity for error_handler_checker
		},
	}

	// Loop structure: buggy_loop → expensive_llm → api_tool → duplicate_llm → buggy_loop
	edges = append(edges,
		domain.Edge{From: "buggy_loop", To: "expensive_llm"},        // unconditional
		domain.Edge{From: "expensive_llm", To: "api_tool"},           // unconditional
		domain.Edge{From: "api_tool", To: "duplicate_llm"},           // unconditional (api_tool has only unconditional outgoing → error_handler_checker)
		domain.Edge{From: "duplicate_llm", To: "buggy_loop"},         // back edge — creates cycle
	)

	// === Subgraph 2: PII leak (triggers pii_leak_scanner) ===

	// RAG source
	nodes["buggy_rag"] = &domain.Node{
		ID:   "buggy_rag",
		Name: "user_data_rag",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "rag",
		},
	}

	// External API sink (no Human gate between rag and api_sink)
	nodes["buggy_api_sink"] = &domain.Node{
		ID:   "buggy_api_sink",
		Name: "external_api_sink",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "api",
		},
	}

	// PII leak path: buggy_rag → buggy_api_sink (no Human gate)
	edges = append(edges,
		domain.Edge{From: "buggy_rag", To: "buggy_api_sink"},
	)

	// Connect entry to PII subgraph from the loop subgraph
	edges = append(edges,
		domain.Edge{From: "buggy_loop", To: "buggy_rag"},
	)

	// === Subgraph 3: Unreachable node (triggers unreachable_node) ===

	// Dangling LLM — no incoming or outgoing edges, completely isolated
	nodes["dangling_node"] = &domain.Node{
		ID:   "dangling_node",
		Name: "dangling_node",
		Type: domain.NodeTypeLLM, // LLM type → Warning (not Info)
		Config: map[string]any{
			"model": "claude-3-haiku",
		},
	}
	// No edges to/from dangling_node — triggers unreachable_node Warning

	// Output node (reachable via pii path)
	nodes["buggy_output"] = &domain.Node{
		ID:     "buggy_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}
	edges = append(edges,
		domain.Edge{From: "buggy_api_sink", To: "buggy_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "buggy_loop",
	}
}

// GeneratePromptInjectionSinkGraph generates a WorkflowGraph that triggers
// the prompt_injection_sink rule with a Critical finding.
//
// Structure:
//   - entry user_query (Tool, Config["source"]="user_input") →
//     llm_assistant (LLM, Config["system_prompt"]="...{{user_query}}...") →
//     output
//
// Expected findings:
//   - prompt_injection_sink: Critical (Confidence 0.9, ConfidenceReason
//     heuristic_pattern). Both source and sink classifications fire (Config
//     hint + name hint on the source; system_prompt + substitution on the
//     sink), and the path is direct.
func GeneratePromptInjectionSinkGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// User-input source — Config["source"] hint AND name pattern both fire.
	nodes["user_query"] = &domain.Node{
		ID:   "user_query",
		Name: "user_query",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"source":      "user_input",
			"description": "raw user prompt from chat UI",
		},
	}

	// LLM sink whose system_prompt directly substitutes the user input.
	nodes["llm_assistant"] = &domain.Node{
		ID:   "llm_assistant",
		Name: "llm_assistant",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "You are a helpful assistant. User said: {{user_query}}. Follow the instructions above strictly.",
		},
	}

	// Output node
	nodes["pi_output"] = &domain.Node{
		ID:     "pi_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "user_query", To: "llm_assistant"},
		domain.Edge{From: "llm_assistant", To: "pi_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "user_query",
	}
}

// GenerateHighFanOutGraph generates a WorkflowGraph with a single orchestrator node
// that fans out to `fanout` worker nodes, triggering the max_parallel_branches rule.
//
// Expected findings depend on the fanout value:
//   - fanout >= 100 → max_parallel_branches: Critical (Confidence=1.0)
//   - fanout >= 20  → max_parallel_branches: Warning  (Confidence=0.9)
//   - fanout >= 10  → max_parallel_branches: Info     (Confidence=0.7)
//   - fanout < 10   → no findings
func GenerateHighFanOutGraph(seed int64, fanout int) *domain.WorkflowGraph {
	_ = seed // deterministic pattern; seed reserved for future randomization

	if fanout < 0 {
		fanout = 0
	}

	nodes := make(map[string]*domain.Node, fanout+2)
	edges := make([]domain.Edge, 0, fanout*2)

	// Orchestrator node — no max_concurrency, so max_parallel_branches applies
	nodes["orchestrator"] = &domain.Node{
		ID:   "orchestrator",
		Name: "ParallelOrchestrator",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model": "gemini-2.0-flash-001",
		},
	}

	// Worker nodes
	for i := 0; i < fanout; i++ {
		workerID := fmt.Sprintf("worker_%03d", i)
		nodes[workerID] = &domain.Node{
			ID:   workerID,
			Name: workerID,
			Type: domain.NodeTypeLLM,
			Config: map[string]any{
				"model": "gpt-4o-mini",
			},
		}
		edges = append(edges, domain.Edge{From: "orchestrator", To: workerID})
	}

	// Aggregator output node
	nodes["aggregator"] = &domain.Node{
		ID:     "aggregator",
		Name:   "ResultAggregator",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	// Each worker converges to aggregator
	for i := 0; i < fanout; i++ {
		workerID := fmt.Sprintf("worker_%03d", i)
		edges = append(edges, domain.Edge{From: workerID, To: "aggregator"})
	}

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "orchestrator",
	}
}

// GenerateTemperatureMisuseGraph generates a WorkflowGraph that triggers the
// temperature_misuse rule with a Warning finding.
//
// Structure:
//   - entry LLM node configured for structured JSON output but temperature > 0
//
// Expected findings:
//   - temperature_misuse: Warning (structured_output=true alongside temperature=0.7)
func GenerateTemperatureMisuseGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// LLM node that asks for structured JSON output but leaves the sampler hot.
	// Triggers temperature_misuse Warning (signal #1: structured_output=true,
	// confidence 0.9, ReasonExactStaticMatch).
	nodes["temp_extractor"] = &domain.Node{
		ID:   "temp_extractor",
		Name: "json_field_extractor",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":             "gpt-4o-mini",
			"temperature":       0.7,
			"structured_output": true,
			"prompt_template":   "extract_invoice_fields",
		},
	}

	// Output node
	nodes["temp_output"] = &domain.Node{
		ID:     "temp_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "temp_extractor", To: "temp_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "temp_extractor",
	}
}

// GenerateEvalMissingGraph generates a WorkflowGraph that triggers the
// eval_missing rule with a Critical finding.
//
// Structure:
//   - entry LLM node → Tool node with Config["category"]="code_execution"
//   - The LLM output flows directly into a code-execution sink with no
//     Condition node and no Human approver, so the rule fires Critical.
//
// Expected findings:
//   - eval_missing: Critical (Confidence 0.9, ConfidenceReason
//     heuristic_pattern). The path LLM → eval_tool has no validation gate
//     between the model and the runner.
func GenerateEvalMissingGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// LLM node — Source
	nodes["plan_llm"] = &domain.Node{
		ID:   "plan_llm",
		Name: "plan_llm",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-4o-mini",
			"prompt_template": "Generate a Python snippet that solves the user's request.",
		},
	}

	// Code-execution Tool — Sink (Config["category"] = "code_execution")
	nodes["python_runner"] = &domain.Node{
		ID:   "python_runner",
		Name: "python_runner",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category":    "code_execution",
			"tool":        "python_runner",
			"description": "Executes generated Python with eval()",
		},
	}

	// Output node
	nodes["eval_output"] = &domain.Node{
		ID:     "eval_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "plan_llm", To: "python_runner"},
		domain.Edge{From: "python_runner", To: "eval_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "plan_llm",
	}
}

// GenerateDynamicNodeConstructionGraph generates a WorkflowGraph that
// triggers the dynamic_node_construction rule with a Critical finding.
//
// Structure:
//   - entry Tool node whose Config["body"] contains a literal `eval(...)`
//     call — this is the LangGraph anti-pattern
//     `add_node(name, lambda x: eval(x))`.
//
// Expected findings:
//   - dynamic_node_construction: Critical (Confidence 0.95,
//     ConfidenceReason exact_static_match). The body field literally
//     contains "eval(" so the strongest pattern fires.
func GenerateDynamicNodeConstructionGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// Tool node with a lambda that calls eval() on its argument.
	nodes["dynamic_dispatcher"] = &domain.Node{
		ID:   "dynamic_dispatcher",
		Name: "dynamic_dispatcher",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "transform",
			// Triggers dynamic_node_construction Critical (eval_call pattern).
			"body": "lambda payload: eval(payload['code'])",
		},
	}

	// Output node
	nodes["dyn_output"] = &domain.Node{
		ID:     "dyn_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "dynamic_dispatcher", To: "dyn_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "dynamic_dispatcher",
	}
}

// GenerateModelCardMismatchGraph generates a WorkflowGraph that triggers the
// model_card_mismatch rule with a Critical finding.
//
// Structure:
//   - entry LLM node declaring model="gpt-4o" but base_url pointing to
//     api.anthropic.com — the runtime call will fail.
//
// Expected findings:
//   - model_card_mismatch: Critical (gpt-* model on Anthropic endpoint)
func GenerateModelCardMismatchGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// LLM node with mismatched model and base_url. gpt-4o belongs to OpenAI,
	// but base_url points to api.anthropic.com — Critical.
	nodes["mismatch_llm"] = &domain.Node{
		ID:   "mismatch_llm",
		Name: "wired_to_wrong_provider",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-4o",
			"base_url":        "https://api.anthropic.com/v1",
			"prompt_template": "general_chat",
		},
	}

	// Output node
	nodes["mismatch_output"] = &domain.Node{
		ID:     "mismatch_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "mismatch_llm", To: "mismatch_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "mismatch_llm",
	}
}
func GenerateCircularDepAgentsGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	nodes["planner_agent"] = &domain.Node{
		ID:   "planner_agent",
		Name: "PlannerAgent",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":      "gpt-4o-mini",
			"agent_role": "planner",
		},
	}

	nodes["worker_agent"] = &domain.Node{
		ID:   "worker_agent",
		Name: "WorkerAgent",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":      "gpt-4o-mini",
			"agent_role": "worker",
		},
	}

	// 2-agent delegation cycle.
	edges = append(edges,
		domain.Edge{From: "planner_agent", To: "worker_agent"},
		domain.Edge{From: "worker_agent", To: "planner_agent"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "planner_agent",
	}
}

// GenerateRetryStormGraph generates a WorkflowGraph that triggers the
// retry_storm rule with a Critical finding.
//
// Structure:
//   - entry orchestrator LLM →
//     storm_api Tool with retries=5 and max_concurrency=20
//     (blast = 5 × 20 = 100 → Critical)
//
// Expected findings:
//   - retry_storm: Critical (Confidence 0.9, ConfidenceReason
//     exact_static_match)
func GenerateRetryStormGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// Orchestrator (drives the storm_api). Plain LLM, no retry config.
	nodes["orchestrator"] = &domain.Node{
		ID:   "orchestrator",
		Name: "ParallelOrchestrator",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model": "gpt-4o-mini",
		},
	}

	// Tool node with retries=5 and max_concurrency=20 → blast 100 → Critical.
	nodes["storm_api"] = &domain.Node{
		ID:   "storm_api",
		Name: "external_api_caller",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category":        "api",
			"retries":         5,
			"max_concurrency": 20,
			"description":     "no exponential backoff, no circuit breaker — storms upstream on failure",
		},
	}

	// Output node
	nodes["storm_output"] = &domain.Node{
		ID:     "storm_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "orchestrator", To: "storm_api"},
		domain.Edge{From: "storm_api", To: "storm_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "orchestrator",
	}
}

func GenerateMissingEvalDatasetGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// Production-flagged orchestrator with NO eval_dataset anywhere.
	nodes["prod_orchestrator"] = &domain.Node{
		ID:   "prod_orchestrator",
		Name: "prod_orchestrator",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":      "gpt-4o-mini",
			"deployment": true,
			"env":        "prod",
		},
	}

	nodes["prod_output"] = &domain.Node{
		ID:     "prod_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "prod_orchestrator", To: "prod_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "prod_orchestrator",
	}
}

// GenerateSecretInPromptTemplateGraph generates a WorkflowGraph that
// triggers the secret_in_prompt_template rule with a Critical finding.
//
// Structure:
//   - entry LLM node whose Config["system_prompt"] contains a hardcoded
//     OpenAI-style API key.
//
// Expected findings:
//   - secret_in_prompt_template: Critical (sk- key in system_prompt,
//     Confidence 0.95, ConfidenceReason exact_static_match)
//   - secret_exposure_scanner: also fires (OnAny recursive scan); not a
//     bug — the two rules carry different Suggestion text and severities,
//     and they apply on different scopes per ADR design.
func GenerateSecretInPromptTemplateGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// LLM node with a secret in its system_prompt — triggers
	// secret_in_prompt_template Critical (Confidence 0.95).
	nodes["leaky_assistant"] = &domain.Node{
		ID:   "leaky_assistant",
		Name: "leaky_assistant",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":         "gpt-4o-mini",
			"system_prompt": "You are an assistant. Use API key sk-abcdefghijklmnopqrstuvwxyz1234567890 for downstream calls.",
		},
	}

	nodes["leak_output"] = &domain.Node{
		ID:     "leak_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "leaky_assistant", To: "leak_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "leaky_assistant",
	}
}

// GenerateUnboundedToolArgGraph generates a WorkflowGraph that triggers the
// unbounded_tool_arg rule with at least one Warning finding.
//
// Structure:
//   - entry Tool node whose Config["args_schema"] declares a string field
//     `query` and an array field `tags` neither of which has an upper bound.
//
// Expected findings:
//   - unbounded_tool_arg: Warning × 2 (string `query` without maxLength,
//     array `tags` without maxItems). ConfidenceReason heuristic_pattern.
func GenerateUnboundedToolArgGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	// Tool node with an unbounded args_schema — triggers unbounded_tool_arg.
	nodes["unbounded_tool"] = &domain.Node{
		ID:   "unbounded_tool",
		Name: "search_tool",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "api",
			"args_schema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "search query",
					},
					"tags": map[string]any{
						"type": "array",
						"items": map[string]any{
							"type":      "string",
							"maxLength": 64.0, // bounded inner string is fine
						},
					},
				},
			},
		},
	}

	// Output node
	nodes["unbounded_output"] = &domain.Node{
		ID:     "unbounded_output",
		Name:   "output",
		Type:   domain.NodeTypeOutput,
		Config: map[string]any{},
	}

	edges = append(edges,
		domain.Edge{From: "unbounded_tool", To: "unbounded_output"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "unbounded_tool",
	}
}

// GenerateN8nGraph generates a WorkflowGraph that mirrors the shape of an
// n8n workflow export — a webhook trigger feeding an LLM (n8n's openAi
// node) which fans out to an HTTP Request tool. The shape exists so tests
// can verify Shingan rules behave the same way against n8n-derived graphs
// as they do against ADK-Go / LangGraph inputs.
//
// Structure:
//   - Webhook (Tool, category=trigger) → ChatGPT (LLM) → HTTP Request (Tool, category=api)
//
// Expected findings: error_handler_checker fires on the trigger and on the
// LLM (the LLM uses a Tool but has no conditional outgoing edge).
func GenerateN8nGraph(seed int64) *domain.WorkflowGraph {
	_ = seed // deterministic pattern

	nodes := make(map[string]*domain.Node)
	var edges []domain.Edge

	nodes["Webhook"] = &domain.Node{
		ID:   "Webhook",
		Name: "Webhook",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "trigger",
			"n8n_type": "n8n-nodes-base.webhook",
		},
	}

	nodes["ChatGPT"] = &domain.Node{
		ID:   "ChatGPT",
		Name: "ChatGPT",
		Type: domain.NodeTypeLLM,
		Config: map[string]any{
			"model":           "gpt-4o-mini",
			"prompt_template": "Summarize the user's request",
			"n8n_type":        "n8n-nodes-base.openAi",
		},
	}

	nodes["HTTP Request"] = &domain.Node{
		ID:   "HTTP Request",
		Name: "HTTP Request",
		Type: domain.NodeTypeTool,
		Config: map[string]any{
			"category": "api",
			"n8n_type": "n8n-nodes-base.httpRequest",
		},
	}

	edges = append(edges,
		domain.Edge{From: "Webhook", To: "ChatGPT"},
		domain.Edge{From: "ChatGPT", To: "HTTP Request"},
	)

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: "Webhook",
	}
}
