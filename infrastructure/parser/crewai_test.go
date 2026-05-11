package parser_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/infrastructure/parser"
)

// findCrewAIShim mirrors findShim but resolves the CrewAI shim. We do not
// reuse parser.LocateShimNamed because that walks from CWD; tests run from
// the package directory, and walking up still works, but an explicit helper
// keeps the failure message friendlier when the worktree is malformed.
func findCrewAIShim(t *testing.T) string {
	t.Helper()
	candidates := []string{
		filepath.Join("infrastructure", "parser", "shims", "export_crewai_server.py"),
		filepath.Join("scripts", "export_crewai_server.py"),
	}
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		for _, rel := range candidates {
			p := filepath.Join(dir, rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate export_crewai_server.py from %q (looked in %v)", dir, candidates)
		}
		dir = parent
	}
}

// requirePythonCrewAI skips the test when crewai is not installed. Mirrors
// requirePythonLangGraph in python_worker_test.go.
func requirePythonCrewAI(t *testing.T) {
	t.Helper()
	requirePython(t)
	cmd := exec.Command("python3", "-c", "import crewai")
	if err := cmd.Run(); err != nil {
		t.Skipf("python3 -c 'import crewai' failed (crewai not installed): %v", err)
	}
}

func TestCrewAIParser_SupportedFormat(t *testing.T) {
	requirePython(t)
	p, err := parser.NewCrewAIParser(parser.WithCrewAIScriptPath(findCrewAIShim(t)))
	if err != nil {
		t.Fatalf("NewCrewAIParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "crewai" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "crewai")
	}
}

func TestCrewAIParser_PythonUnavailable(t *testing.T) {
	_, err := parser.NewCrewAIParser(
		parser.WithCrewAIScriptPath(findCrewAIShim(t)),
		parser.WithCrewAIPythonBinary("python_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when python is not available")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
}

func TestCrewAIParser_CrewAIMissing(t *testing.T) {
	requirePython(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	cmd := exec.Command("python3", "-c", "import crewai")
	if err := cmd.Run(); err == nil {
		t.Skip("crewai IS installed in this environment; this test only runs when crewai is missing")
	}

	p, err := parser.NewCrewAIParser(parser.WithCrewAIScriptPath(findCrewAIShim(t)))
	if err != nil {
		t.Fatalf("NewCrewAIParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, parseErr := p.Parse([]byte("from crewai import Crew\n"))
	if parseErr == nil {
		t.Fatal("expected error when crewai is missing")
	}
	if !strings.Contains(parseErr.Error(), "pip install crewai") {
		t.Errorf("error %q does not mention `pip install crewai`", parseErr)
	}
}

func TestCrewAIParser_LocateShimNamed(t *testing.T) {
	// The exported LocateShimNamed function should find the CrewAI shim by
	// walking up from the current working directory. This guards against
	// regressions where the env-var name derivation breaks.
	path, err := parser.LocateShimNamed("export_crewai_server.py")
	if err != nil {
		t.Fatalf("LocateShimNamed: %v", err)
	}
	if !strings.HasSuffix(path, "shims/export_crewai_server.py") &&
		!strings.HasSuffix(path, "scripts/export_crewai_server.py") {
		t.Errorf("path %q does not end in shims/ or scripts/ export_crewai_server.py", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("located path does not exist: %v", err)
	}
}

// ---- integration tests below this point require `pip install crewai` -----

func TestCrewAIParser_SimpleSequential(t *testing.T) {
	requirePythonCrewAI(t)
	p, err := parser.NewCrewAIParser(parser.WithCrewAIScriptPath(findCrewAIShim(t)))
	if err != nil {
		t.Fatalf("NewCrewAIParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	// Inline source — minimal sequential crew with two agents/tasks.
	src := `
from crewai import Agent, Task, Crew, Process

researcher = Agent(role="researcher", goal="research stuff", backstory="b1", allow_delegation=False)
writer     = Agent(role="writer",     goal="write stuff",    backstory="b2", allow_delegation=False)

t1 = Task(description="find sources", expected_output="list", agent=researcher)
t2 = Task(description="write report", expected_output="md",   agent=writer)

crew = Crew(agents=[researcher, writer], tasks=[t1, t2], process=Process.sequential)
`
	graph, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Nodes) < 4 { // 2 agents + 2 tasks at minimum
		nodeIDs := make([]string, 0, len(graph.Nodes))
		for k := range graph.Nodes {
			nodeIDs = append(nodeIDs, k)
		}
		t.Errorf("expected >=4 nodes, got %d (nodes=%v)", len(graph.Nodes), nodeIDs)
	}
	if graph.EntryNodeID == "" {
		t.Error("expected non-empty EntryNodeID")
	}
	// Sequential should produce at least one task→task edge.
	taskTaskEdges := 0
	for _, e := range graph.Edges {
		if strings.Contains(e.From, "::task::") && strings.Contains(e.To, "::task::") {
			taskTaskEdges++
		}
	}
	if taskTaskEdges < 1 {
		t.Errorf("expected at least one task→task sequential edge, got %d", taskTaskEdges)
	}
}

func TestCrewAIParser_CircularDelegation(t *testing.T) {
	requirePythonCrewAI(t)
	p, err := parser.NewCrewAIParser(parser.WithCrewAIScriptPath(findCrewAIShim(t)))
	if err != nil {
		t.Fatalf("NewCrewAIParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	// Two agents both with allow_delegation=True → bidirectional delegation
	// edges so circular_dep_agents rule has structural evidence.
	src := `
from crewai import Agent, Task, Crew, Process

a = Agent(role="alpha", goal="g", backstory="b", allow_delegation=True)
b = Agent(role="beta",  goal="g", backstory="b", allow_delegation=True)

t1 = Task(description="task1", expected_output="out", agent=a)
t2 = Task(description="task2", expected_output="out", agent=b)

crew = Crew(agents=[a, b], tasks=[t1, t2], process=Process.sequential)
`
	graph, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	delegateEdges := 0
	for _, e := range graph.Edges {
		if e.Condition == "delegate" {
			delegateEdges++
		}
	}
	if delegateEdges < 2 {
		t.Errorf("expected >=2 delegate edges (bidirectional), got %d", delegateEdges)
	}
}

// TestCrewAIParser_AgentsOnlyModuleSkipped locks in the v0.8.7 FP-2
// fix: a definition-only module (Agent ctors only, no Task / Crew)
// should produce an empty graph via the AST fallback so the
// `unreachable_node` rule has nothing to flag. Mirrors Devyan's
// agents.py — without this guard, the first agent became entry and
// the other 3 were wrongly flagged as unreachable.
func TestCrewAIParser_AgentsOnlyModuleSkipped(t *testing.T) {
	requirePythonCrewAI(t)
	p, err := parser.NewCrewAIParser(parser.WithCrewAIScriptPath(findCrewAIShim(t)))
	if err != nil {
		t.Fatalf("NewCrewAIParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	// Agent factory methods inside a class — no top-level Crew, so the
	// live worker can't materialise a graph and falls back to AST.
	src := `
from crewai import Agent

class CustomAgents:
    def architect_agent(self, tools):
        return Agent(role="Software Architect", goal="g", backstory="b",
                     tools=tools, allow_delegation=False)
    def programmer_agent(self, tools):
        return Agent(role="Software Programmer", goal="g", backstory="b",
                     tools=tools, allow_delegation=False)
    def tester_agent(self, tools):
        return Agent(role="Software Tester", goal="g", backstory="b",
                     tools=tools, allow_delegation=False)
`
	graph, err := p.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(graph.Nodes) != 0 {
		nodeIDs := make([]string, 0, len(graph.Nodes))
		for k := range graph.Nodes {
			nodeIDs = append(nodeIDs, k)
		}
		t.Errorf("expected 0 nodes for agents-only module, got %d: %v", len(graph.Nodes), nodeIDs)
	}
	if len(graph.Edges) != 0 {
		t.Errorf("expected 0 edges for agents-only module, got %d", len(graph.Edges))
	}
}
