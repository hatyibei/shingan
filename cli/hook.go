package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// newHookCmd groups the editor/agent hook integration: `hook run` is the hook
// body an agent invokes after writing a file; `hook install` wires it into the
// agent's settings. The design goal (ADR-019 distribution) is zero-friction:
// an AI agent that just generated a workflow gets it statically checked, and
// is blocked on a Critical finding so it self-corrects before the code ships.
func newHookCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hook",
		Short: "Run shingan as an editor/agent hook, or install it into agent settings",
		Long: `hook integrates shingan into an AI coding agent's edit loop.

  shingan hook install        register the PostToolUse hook in .claude/settings.json
  shingan hook run            the hook body (reads the tool event on stdin)

After install, whenever the agent writes or edits a workflow file, shingan
analyzes it with --format auto. A Critical finding (infinite loop, cost
explosion, missing error path) exits 2 with the reason on stderr, which the
agent reads and self-corrects. Warnings are surfaced but never block.`,
	}
	cmd.AddCommand(newHookRunCmd())
	cmd.AddCommand(newHookInstallCmd())
	return cmd
}

// hookExtensions is the cheap pre-filter: files shingan could possibly parse.
var hookExtensions = map[string]bool{
	".py": true, ".ts": true, ".mts": true, ".cts": true, ".tsx": true,
	".js": true, ".mjs": true, ".cjs": true, ".jsx": true, ".go": true, ".json": true,
}

func newHookRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "PostToolUse hook body: analyze the just-saved file, block on Critical",
		Long: `run reads a Claude Code PostToolUse event as JSON on stdin, extracts
tool_input.file_path, and analyzes that file with --format auto.

Exit codes follow the hook contract:
  0  clean / info / warning-only / not a workflow file → agent proceeds
  2  Critical finding → reason printed to stderr, agent is asked to fix it`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runHook(cmd)
		},
	}
}

// runHook is the hook body. It never returns a hard error for malformed input
// or non-workflow files — that would block the agent on unrelated edits. It
// blocks (exitCodeError{2}) only on a genuine Critical finding.
func runHook(cmd *cobra.Command) error {
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return nil
	}
	var payload struct {
		ToolInput struct {
			FilePath string `json:"file_path"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil // malformed event → don't block
	}
	file := payload.ToolInput.FilePath
	if file == "" || !hookExtensions[strings.ToLower(filepath.Ext(file))] {
		return nil
	}
	if fi, err := os.Stat(file); err != nil || fi.IsDir() {
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return nil
	}
	report, code := analyzeFileSubprocess(self, file)
	stderr := cmd.ErrOrStderr()
	base := filepath.Base(file)

	switch code {
	case 2:
		fmt.Fprintf(stderr, "🛑 shingan blocked: CRITICAL workflow issue in %s — fix before continuing.\n\n", base)
		if crit := criticalSection(report); crit != "" {
			fmt.Fprintln(stderr, crit)
		}
		fmt.Fprintln(stderr, "Resolve the Critical finding above (add the recursion_limit / max_iterations / error-branch guard the suggestion names), then re-save. These are run-before-execution checks; shipping past them risks an infinite loop or cost explosion at runtime.")
		return &exitCodeError{code: 2}
	case 1:
		fmt.Fprintf(stderr, "shingan: warning-level finding(s) in %s (non-blocking).\n", base)
	}
	return nil
}

// analyzeFileSubprocess re-invokes this same binary as `analyze --format auto`,
// so the hook reuses the full analyzer pipeline (parsers, rules, format
// detection) instead of duplicating it. Returns the markdown report and the
// analyze exit code (0/1/2; anything else — e.g. unknown format — maps to 0 so
// the hook stays quiet on files shingan can't parse).
func analyzeFileSubprocess(self, file string) (string, int) {
	c := exec.Command(self, "analyze", "--format", "auto", "--input", file, "--output", "markdown")
	var out strings.Builder
	c.Stdout = &out
	c.Stderr = io.Discard
	err := c.Run()
	if err == nil {
		return out.String(), 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		code := ee.ExitCode()
		if code == 1 || code == 2 {
			return out.String(), code
		}
	}
	return "", 0
}

// criticalSection extracts the "## Critical" block from a markdown report
// (up to the next "## " heading) so the agent sees only what blocks it.
func criticalSection(md string) string {
	lines := strings.Split(md, "\n")
	var b strings.Builder
	in := false
	for _, ln := range lines {
		if strings.HasPrefix(ln, "## Critical") {
			in = true
			b.WriteString(ln + "\n")
			continue
		}
		if in && strings.HasPrefix(ln, "## ") {
			break
		}
		if in {
			b.WriteString(ln + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

func newHookInstallCmd() *cobra.Command {
	var global bool
	var agent, binName string
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Register shingan's PostToolUse hook in your agent's settings.json",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if agent != "claude-code" {
				return fmt.Errorf("unsupported --agent %q (only \"claude-code\" is supported today)", agent)
			}
			path := claudeSettingsPath(global)
			added, err := installClaudeHook(path, binName)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if added {
				fmt.Fprintf(out, "✓ Installed shingan PostToolUse hook → %s\n", path)
				fmt.Fprintf(out, "  Command: %s hook run  (matcher: Edit|Write)\n", binName)
				fmt.Fprintln(out, "  The agent will now run shingan on saved workflow files and block on Critical findings.")
			} else {
				fmt.Fprintf(out, "• shingan PostToolUse hook already present in %s (no change)\n", path)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "Install to ~/.claude/settings.json (default: ./.claude/settings.json)")
	cmd.Flags().StringVar(&agent, "agent", "claude-code", "Target agent (claude-code)")
	cmd.Flags().StringVar(&binName, "bin", "shingan", "Command invoked by the hook (a name on PATH or an absolute path)")
	return cmd
}

func claudeSettingsPath(global bool) string {
	if global {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, ".claude", "settings.json")
		}
	}
	return filepath.Join(".claude", "settings.json")
}

// installClaudeHook merges a PostToolUse hook into a Claude Code settings.json,
// preserving every other key and existing hook. Returns (added=false) when an
// identical shingan hook is already registered (idempotent). The hook command
// is "<bin> hook run".
func installClaudeHook(path, binName string) (bool, error) {
	command := binName + " hook run"

	settings := map[string]any{}
	if raw, err := os.ReadFile(path); err == nil {
		if len(strings.TrimSpace(string(raw))) > 0 {
			if err := json.Unmarshal(raw, &settings); err != nil {
				return false, fmt.Errorf("parse %s: %w (fix or remove it, then retry)", path, err)
			}
		}
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("read %s: %w", path, err)
	}

	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	postArr, _ := hooks["PostToolUse"].([]any)

	// Idempotency: bail if any PostToolUse entry already runs our command.
	for _, e := range postArr {
		m, _ := e.(map[string]any)
		inner, _ := m["hooks"].([]any)
		for _, h := range inner {
			hm, _ := h.(map[string]any)
			if cmdStr, _ := hm["command"].(string); cmdStr == command {
				return false, nil
			}
		}
	}

	postArr = append(postArr, map[string]any{
		"matcher": "Edit|Write",
		"hooks": []any{
			map[string]any{"type": "command", "command": command, "timeout": 30},
		},
	})
	hooks["PostToolUse"] = postArr
	settings["hooks"] = hooks

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("create settings dir: %w", err)
	}
	enc, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(path, append(enc, '\n'), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}
