package parser_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/parser"
)

// osStat is a tiny indirection so the test file's import block stays minimal.
var osStat = os.Stat

// findTestdataDir returns the absolute path to testdata/langgraph/, walking
// upwards from the current test working directory.
func findTestdataDir(t *testing.T) string {
	t.Helper()
	dir, err := filepathAbs(".")
	if err != nil {
		t.Fatalf("abs cwd: %v", err)
	}
	for {
		p := filepath.Join(dir, "testdata", "langgraph")
		if isDir(p) {
			return p
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate testdata/langgraph from %q", dir)
		}
		dir = parent
	}
}

// filepathAbs / isDir are wrappers kept tiny so the import block stays clean.
func filepathAbs(p string) (string, error) { return filepath.Abs(p) }
func isDir(p string) bool {
	info, err := osStat(p)
	return err == nil && info.IsDir()
}

func TestLangGraphParser_SupportedFormat(t *testing.T) {
	requirePython(t)
	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if got := p.SupportedFormat(); got != "langgraph" {
		t.Errorf("SupportedFormat() = %q, want %q", got, "langgraph")
	}
}

func TestLangGraphParser_PythonUnavailable(t *testing.T) {
	// Force a bad Python binary so the worker can't even start. Either the
	// spawn fails outright (preferred path) or the very first call surfaces
	// the missing-binary error.
	_, err := parser.NewLangGraphParser(
		parser.WithLangGraphScriptPath(findShim(t)),
		parser.WithLangGraphPythonBinary("python_does_not_exist_xyz_42"),
	)
	if err == nil {
		t.Fatal("expected error when python is not available")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q does not mention PATH", err)
	}
}

func TestLangGraphParser_LangGraphMissing(t *testing.T) {
	requirePython(t)
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not found")
	}
	cmd := exec.Command("python3", "-c", "import langgraph")
	if err := cmd.Run(); err == nil {
		t.Skip("langgraph IS installed in this environment; this test only runs when langgraph is missing")
	}

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	_, parseErr := p.Parse([]byte("from langgraph.graph import StateGraph\n"))
	if parseErr == nil {
		t.Fatal("expected error when langgraph is missing")
	}
	if !strings.Contains(parseErr.Error(), "pip install langgraph") {
		t.Errorf("error %q does not mention `pip install langgraph`", parseErr)
	}
}

// integration tests below all require a live `python3 -c 'import langgraph'`.
// They share the parser instance via t.Cleanup so each test still gets its
// own subprocess (no state leakage).

func TestLangGraphParser_SimpleChain(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "simple_chain.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// START / END are pseudo-sentinels in LangGraph; we elide them so that
	// Shingan's `loop_guard` doesn't misclassify the START node as a LoopAgent.
	wantNodes := []string{"classify", "respond"}
	for _, id := range wantNodes {
		if _, ok := graph.Nodes[id]; !ok {
			t.Errorf("missing expected node %q", id)
		}
	}
	for _, sentinel := range []string{"__start__", "__end__"} {
		if _, ok := graph.Nodes[sentinel]; ok {
			t.Errorf("sentinel %q must not appear as a node", sentinel)
		}
	}
	if got := graph.EntryNodeID; got != "classify" {
		t.Errorf("EntryNodeID = %q, want \"classify\"", got)
	}
	// classify -> respond, single inter-node edge (START/END dropped).
	if got, want := len(graph.Edges), 1; got != want {
		t.Errorf("len(Edges) = %d, want %d", got, want)
	}
	for _, id := range []string{"classify", "respond"} {
		n := graph.Nodes[id]
		if n.Type != domain.NodeTypeLLM {
			t.Errorf("node %q Type = %v, want NodeTypeLLM", id, n.Type)
		}
	}
}

func TestLangGraphParser_Branching(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "branching.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// Conditional edges are over-approximated → triage should fan out to all
	// three workers, each via a labelled edge.
	conditional := 0
	for _, e := range graph.Edges {
		if e.From == "triage" && e.Condition != "" {
			conditional++
		}
	}
	if conditional < 3 {
		t.Errorf("expected at least 3 conditional edges from triage, got %d", conditional)
	}
}

func TestLangGraphParser_ReactLoop(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "react_loop.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// model→tools and tools→model both present → cycle.
	hasMT, hasTM := false, false
	for _, e := range graph.Edges {
		if e.From == "model" && e.To == "tools" {
			hasMT = true
		}
		if e.From == "tools" && e.To == "model" {
			hasTM = true
		}
	}
	if !hasMT || !hasTM {
		t.Errorf("expected model⇄tools cycle, got hasMT=%v hasTM=%v", hasMT, hasTM)
	}
	if n, ok := graph.Nodes["tools"]; ok && n.Type != domain.NodeTypeTool {
		t.Errorf("tools node Type = %v, want NodeTypeTool", n.Type)
	}
}

func TestLangGraphParser_MultiAgent(t *testing.T) {
	requirePythonLangGraph(t)
	dir := findTestdataDir(t)

	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	graph, err := p.ParseFile(filepath.Join(dir, "multi_agent.py"))
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	// All workers loop back to supervisor.
	loopBacks := 0
	for _, e := range graph.Edges {
		if e.To == "supervisor" && e.From != "__start__" {
			loopBacks++
		}
	}
	if loopBacks < 3 {
		t.Errorf("expected >= 3 worker→supervisor loopback edges, got %d", loopBacks)
	}
}

func TestLangGraphParser_WorkerCrashRecovery(t *testing.T) {
	// We don't restart automatically yet (Phase 2-A), but the parser must at
	// least surface a clean error rather than hanging when the worker dies.
	requirePython(t)
	p, err := parser.NewLangGraphParser(parser.WithLangGraphScriptPath(findShim(t)))
	if err != nil {
		t.Fatalf("NewLangGraphParser: %v", err)
	}
	// Force-close mid-session.
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := p.ParseFile("does_not_matter.py"); err == nil {
		t.Fatal("expected error on parse after Close()")
	}
}
