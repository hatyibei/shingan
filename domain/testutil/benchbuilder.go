// Package testutil provides helpers for constructing WorkflowGraph instances
// in tests without requiring a real framework parser.
package testutil

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/hatyibei/shingan/domain"
)

// GenerateRandomGraph generates a reproducible random WorkflowGraph with n nodes.
//
// Node type distribution:
//   - LLM: 40%
//   - Tool: 30%
//   - Control: 10%
//   - Human: 5%
//   - Output: 5%
//   - (remaining ~10%): randomly assigned
//
// Edge density: approximately n^1.2 edges (linear graph backbone + random extras).
//
// The graph intentionally includes:
//   - At least one cycle (exercises cycle_detection, cost_estimation loop logic)
//   - Control nodes without max_iterations (exercises loop_guard)
//   - Unreachable nodes (exercises unreachable_node)
//   - Tool nodes with unconditional outgoing edges (exercises error_handler_checker)
//   - Duplicate LLM nodes with same model+prompt_template (exercises redundant_llm_call)
//   - RAG/PII source → external sink paths without Human gates (exercises pii_leak_scanner)
//   - High-cost model LLM nodes inside loops (exercises cost_estimation)
//
// The seed parameter makes the graph fully reproducible across test runs.
func GenerateRandomGraph(n int, seed int64) *domain.WorkflowGraph {
	if n <= 0 {
		return &domain.WorkflowGraph{
			Nodes:       make(map[string]*domain.Node),
			Edges:       []domain.Edge{},
			EntryNodeID: "",
		}
	}

	rng := rand.New(rand.NewSource(seed))

	nodes := make(map[string]*domain.Node, n)
	ids := make([]string, n)

	// Assign node types with the required distribution.
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("node_%04d", i)
		ids[i] = id
		nt := randomNodeType(rng, i, n)
		cfg := buildConfig(rng, nt, i)
		nodes[id] = &domain.Node{
			ID:     id,
			Name:   nameFor(id, nt, i),
			Type:   nt,
			Config: cfg,
		}
	}

	// Entry node is always node_0000.
	entryID := ids[0]

	edges := make([]domain.Edge, 0, int(math.Pow(float64(n), 1.2)))

	// --- Backbone: linear chain from entry through all nodes ---
	// This ensures all nodes are reachable initially.
	// We deliberately skip some nodes (the last 5%) to create unreachable nodes.
	reachableCount := max(1, n-max(1, n/20)) // ~95% reachable
	for i := 0; i < reachableCount-1; i++ {
		edges = append(edges, domain.Edge{From: ids[i], To: ids[i+1]})
	}

	// --- Random extra edges to reach n^1.2 density ---
	targetEdges := int(math.Pow(float64(n), 1.2))
	for len(edges) < targetEdges {
		from := ids[rng.Intn(reachableCount)]
		to := ids[rng.Intn(n)]
		if from != to {
			edges = append(edges, domain.Edge{From: from, To: to})
		}
	}

	// --- Deliberate structural features for rule coverage ---

	// 1. Cycle: node_0002 → node_0001 (back edge into the backbone)
	if n >= 4 {
		edges = append(edges, domain.Edge{From: ids[2], To: ids[1]})
	}

	// 2. High-cost LLM in the cycle region.
	//    node_0001 is in the cycle; ensure it is LLM with high-cost model.
	if n >= 2 {
		nodes[ids[1]].Type = domain.NodeTypeLLM
		nodes[ids[1]].Config = map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "shared_template",
		}
		nodes[ids[1]].Name = ids[1]
	}

	// 3. Duplicate LLM pair (same model + prompt_template as node_0001).
	if n >= 5 {
		nodes[ids[3]].Type = domain.NodeTypeLLM
		nodes[ids[3]].Config = map[string]any{
			"model":           "gpt-4o",
			"prompt_template": "shared_template",
		}
		nodes[ids[3]].Name = ids[3]
	}

	// 4. Loop node without max_iterations in the loop region (triggers loop_guard).
	if n >= 3 {
		nodes[ids[2]].Type = domain.NodeTypeLoop
		nodes[ids[2]].Config = map[string]any{} // no max_iterations
		nodes[ids[2]].Name = ids[2]
	}

	// 5. RAG source → external API sink without Human gate.
	//    Use near-end reachable nodes (index reachableCount-3 and reachableCount-2).
	if reachableCount >= 5 {
		ragIdx := reachableCount - 3
		sinkIdx := reachableCount - 2
		nodes[ids[ragIdx]].Type = domain.NodeTypeTool
		nodes[ids[ragIdx]].Config = map[string]any{"category": "rag"}
		nodes[ids[ragIdx]].Name = "user_data_rag"
		nodes[ids[sinkIdx]].Type = domain.NodeTypeTool
		nodes[ids[sinkIdx]].Config = map[string]any{"category": "api"}
		nodes[ids[sinkIdx]].Name = ids[sinkIdx]
		// Direct edge: rag → api (no Human gate in between)
		edges = append(edges, domain.Edge{From: ids[ragIdx], To: ids[sinkIdx]})
	}

	// 6. Tool node with unconditional outgoing edges (no error handler).
	//    Use index 4 if available.
	if n >= 6 {
		toolIdx := 4
		nodes[ids[toolIdx]].Type = domain.NodeTypeTool
		nodes[ids[toolIdx]].Config = map[string]any{"category": "api"}
		nodes[ids[toolIdx]].Name = ids[toolIdx]
		// Unconditional edges are the backbone edges already added.
	}

	return &domain.WorkflowGraph{
		Nodes:       nodes,
		Edges:       edges,
		EntryNodeID: entryID,
	}
}

// randomNodeType returns a NodeType based on the target distribution.
// Some indices are reserved for deliberate structural nodes (set later).
func randomNodeType(rng *rand.Rand, idx, n int) domain.NodeType {
	// Reserve specific indices for deliberate structure; assign generic types here.
	// Deliberate overrides happen after the main loop in GenerateRandomGraph.
	r := rng.Float64()
	switch {
	case r < 0.40:
		return domain.NodeTypeLLM
	case r < 0.70:
		return domain.NodeTypeTool
	case r < 0.75:
		return domain.NodeTypeLoop
	case r < 0.80:
		return domain.NodeTypeCondition
	case r < 0.85:
		return domain.NodeTypeHuman
	case r < 0.90:
		return domain.NodeTypeOutput
	default:
		// Remaining 10%: cycle through types
		return domain.NodeType(idx % 5)
	}
}

// buildConfig builds a realistic Config map for the given node type.
func buildConfig(rng *rand.Rand, nt domain.NodeType, idx int) map[string]any {
	cfg := make(map[string]any)
	switch nt {
	case domain.NodeTypeLLM:
		models := []string{"gpt-4o", "gpt-4o-mini", "claude-3-haiku", "gemini-1.5-flash"}
		cfg["model"] = models[rng.Intn(len(models))]
		// Only set prompt_template on some nodes (not all, to avoid all being redundant).
		if rng.Float64() < 0.3 {
			cfg["prompt_template"] = fmt.Sprintf("template_%d", idx%5)
		}
	case domain.NodeTypeLoop:
		// ~50% chance of having max_iterations set (the rest trigger loop_guard).
		if rng.Float64() < 0.5 {
			cfg["max_iterations"] = rng.Intn(150) + 1
		}
	case domain.NodeTypeCondition:
		cfg["expression"] = fmt.Sprintf("cond_%d", idx)
	case domain.NodeTypeTool:
		categories := []string{"api", "mcp", "browser", "code", "rag"}
		cfg["category"] = categories[rng.Intn(len(categories))]
	}
	return cfg
}

// nameFor returns a human-readable name for a node.
func nameFor(id string, nt domain.NodeType, idx int) string {
	switch nt {
	case domain.NodeTypeLLM:
		return fmt.Sprintf("llm_%d", idx)
	case domain.NodeTypeTool:
		return fmt.Sprintf("tool_%d", idx)
	case domain.NodeTypeLoop:
		return fmt.Sprintf("loop_%d", idx)
	case domain.NodeTypeCondition:
		return fmt.Sprintf("cond_%d", idx)
	case domain.NodeTypeHuman:
		return fmt.Sprintf("human_%d", idx)
	case domain.NodeTypeOutput:
		return fmt.Sprintf("output_%d", idx)
	default:
		return id
	}
}
