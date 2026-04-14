package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"google.golang.org/adk/agent"
	adkrunner "google.golang.org/adk/runner"
	"google.golang.org/adk/session"
	"google.golang.org/genai"

	"github.com/hatyibei/shingan/application"
	"github.com/hatyibei/shingan/domain"
	"github.com/hatyibei/shingan/infrastructure/factory"
)

// sampleMeta holds metadata about a runnable sample.
type sampleMeta struct {
	name        string
	description string
	sourceFile  string // path relative to project root
}

var knownSamples = map[string]sampleMeta{
	"simple": {
		name:        "simple",
		description: "Minimal LlmAgent — says hello in Japanese",
		sourceFile:  "examples/runtime/simple_agent.go",
	},
	"infinite_loop_bounded": {
		name:        "infinite_loop_bounded",
		description: "LoopAgent with MaxIterations=3 — safe bounded execution",
		sourceFile:  "examples/runtime/infinite_loop_bounded.go",
	},
	"infinite_loop_unbounded": {
		name:        "infinite_loop_unbounded",
		description: "LoopAgent WITHOUT MaxIterations — Shingan Critical, execution refused",
		sourceFile:  "examples/runtime/infinite_loop_unbounded.go",
	},
}

// runSample performs the full demo flow:
//  1. Locate source file for the sample and run Shingan static analysis.
//  2. If Critical findings exist and maxIter==0, refuse execution.
//  3. Otherwise (or with --dry-run) print findings and optionally execute.
//
// Returns a non-nil error only for unexpected failures; safe-guard refusal
// is printed to stdout and the function returns nil.
func runSample(ctx context.Context, sampleName string, maxIter uint, dryRun bool) error {
	meta, ok := knownSamples[sampleName]
	if !ok {
		return fmt.Errorf("unknown sample %q — valid choices: simple, infinite_loop_bounded, infinite_loop_unbounded", sampleName)
	}

	// Resolve source file path.
	projectRoot := findProjectRoot()
	srcPath := projectRoot + "/" + meta.sourceFile

	fmt.Printf("=== Shingan Runner — sample: %s ===\n\n", sampleName)

	// --- Step 1: static analysis ---
	fmt.Printf("[1/3] Running Shingan static analysis on %s ...\n", meta.sourceFile)

	findings, err := analyzeFile(srcPath)
	if err != nil {
		return fmt.Errorf("shingan analysis: %w", err)
	}

	if len(findings) == 0 {
		fmt.Println("    ✓ No findings — clean agent")
		fmt.Println()
	}
	for _, f := range findings {
		icon := "⚠"
		if f.Severity == domain.Critical {
			icon = "✗"
		}
		fmt.Printf("    %s [%s] %s — %s\n", icon, f.Severity, f.RuleName, f.Message)
	}

	// --- Step 2: safe-guard check ---
	hasCritical := hasCriticalFinding(findings)

	if hasCritical && maxIter == 0 {
		fmt.Println("\n[2/3] Safe-guard: Critical finding detected → EXECUTION REFUSED")
		fmt.Println("      Use --max-iter N to override (e.g., --max-iter 3 to bound the loop).")
		fmt.Println("\n    ✗ Execution blocked by Shingan safe-guard")
		return nil
	}

	if hasCritical && maxIter > 0 {
		fmt.Printf("\n[2/3] Safe-guard override: --max-iter %d provided, proceeding with bounded execution.\n", maxIter)
	} else {
		fmt.Println("\n[2/3] Safe-guard: no Critical findings → execution allowed")
	}

	// --- Step 3: execute (unless --dry-run) ---
	if dryRun {
		fmt.Println("\n[3/3] --dry-run: skipping actual execution")
		fmt.Println("      Analysis complete. No Vertex AI calls made.")
		return nil
	}

	fmt.Printf("\n[3/3] Executing agent via Vertex AI Gemini (%s)...\n", geminiModel)

	return executeAgent(ctx, sampleName, maxIter)
}

// analyzeFile runs all Shingan rules on a single .go source file.
func analyzeFile(path string) ([]domain.Finding, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", path, err)
	}

	parserFac := factory.NewParserFactory()
	p, err := parserFac.Create("adk-go")
	if err != nil {
		return nil, fmt.Errorf("create parser: %w", err)
	}

	graph, err := p.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	analyzerFac := factory.NewAnalyzerFactory()
	rules := analyzerFac.CreateAll()

	orch := application.NewAnalysisOrchestrator()
	return orch.Analyze(graph, rules), nil
}

// hasCriticalFinding returns true if any finding has Critical severity.
func hasCriticalFinding(findings []domain.Finding) bool {
	for _, f := range findings {
		if f.Severity == domain.Critical {
			return true
		}
	}
	return false
}

// executeAgent builds and runs the named agent against Vertex AI.
func executeAgent(ctx context.Context, sampleName string, maxIter uint) error {
	var rootAgent agent.Agent
	var err error

	switch sampleName {
	case "simple":
		rootAgent, err = buildSimpleAgent(ctx)
	case "infinite_loop_bounded":
		rootAgent, err = buildInfiniteLoopBounded(ctx, maxIter)
	default:
		return fmt.Errorf("sample %q has no runnable builder (this should not happen after safe-guard)", sampleName)
	}
	if err != nil {
		return fmt.Errorf("build agent: %w", err)
	}

	// Create runner with in-memory session service.
	r, err := adkrunner.New(adkrunner.Config{
		AppName:           "shingan-demo",
		Agent:             rootAgent,
		SessionService:    session.InMemoryService(),
		AutoCreateSession: true,
	})
	if err != nil {
		return fmt.Errorf("create runner: %w", err)
	}

	// Build user message.
	userMsg := genai.NewContentFromText("Start", genai.RoleUser)

	fmt.Println()
	fmt.Println("--- Agent Output ---")

	iterCount := 0
	for event, err := range r.Run(ctx, "demo-user", "session-1", userMsg, agent.RunConfig{}) {
		if err != nil {
			return fmt.Errorf("run error: %w", err)
		}
		if event == nil {
			continue
		}
		// Only print final text events (not function calls, partials, etc.).
		if event.Content != nil && !event.Partial {
			for _, part := range event.Content.Parts {
				if part.Text != "" {
					iterCount++
					fmt.Printf("[event %d / author=%s] %s\n", iterCount, event.Author, part.Text)
				}
			}
		}
	}

	fmt.Println("--- End of Output ---")
	fmt.Printf("\n✓ Agent execution complete. Events printed: %d\n", iterCount)
	return nil
}

// findProjectRoot returns the project root directory.
// Strategy: walk up from cwd looking for go.mod.
func findProjectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	dir := cwd
	for {
		if _, err := os.Stat(dir + "/go.mod"); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root, fall back to cwd.
			return cwd
		}
		dir = parent
	}
}
