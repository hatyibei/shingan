package cli

import (
	"fmt"
	"io"
	"os"
)

// supportedFormatsHint is the human-readable list used in error messages.
const supportedFormatsHint = "json, adk-go, samurai, langgraph, n8n, crewai, langgraph-js, pydantic-graph, llamaindex, autogen, mastra, openai-agents"

// resolveInputFormat turns the --format flag value into a concrete parser
// format.
//
//   - ""     (flag not given): auto-detect from extension + content; when
//     nothing matches, fall back to "json" — the pre-auto default — so every
//     invocation that worked before keeps working.
//   - "auto" (explicit): auto-detect strictly; error when nothing matches so an
//     editor / agent hook can skip unknown files cleanly instead of mis-parsing.
//   - anything else: used as-is.
//
// A one-line notice goes to stderr whenever detection picked something other
// than the plain json default, so the user can see (and override) the choice
// without it polluting the report on stdout.
func resolveInputFormat(flagValue, input string, stderr io.Writer) (string, error) {
	switch flagValue {
	case "":
		detected := detectFormat(input)
		if detected == "" || detected == "json" {
			return "json", nil
		}
		fmt.Fprintf(stderr, "shingan: detected format %q (override with --format)\n", detected)
		return detected, nil
	case "auto":
		detected := detectFormat(input)
		if detected == "" {
			return "", fmt.Errorf(
				"--format auto could not detect a supported workflow format for %q; pass --format explicitly (%s)",
				input, supportedFormatsHint)
		}
		fmt.Fprintf(stderr, "shingan: auto-detected format %q\n", detected)
		return detected, nil
	default:
		return flagValue, nil
	}
}

// resolveOutputFormat turns the --output flag value into a reporter format.
// When unset, humans at a terminal get Markdown and everything else — pipes,
// redirects, --output-file, in-process callers with a buffer — gets JSON, the
// historical default, so scripts and CI see no change.
func resolveOutputFormat(flagValue, outputFile string, stdout io.Writer) string {
	if flagValue != "" {
		return flagValue
	}
	if outputFile == "" && isTerminal(stdout) {
		return "markdown"
	}
	return "json"
}

// isTerminal reports whether w is an *os.File attached to a character device
// (a TTY). A nil writer means "os.Stdout" (see writeOutput).
func isTerminal(w io.Writer) bool {
	if w == nil {
		w = os.Stdout
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
