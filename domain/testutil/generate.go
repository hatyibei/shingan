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
