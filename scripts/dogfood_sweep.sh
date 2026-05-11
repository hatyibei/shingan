#!/usr/bin/env bash
#
# dogfood_sweep.sh — clone the current dogfood corpus and run `shingan analyze`
# on each repo, emitting a per-repo Markdown summary into $OUT_DIR.
#
# This is the script that backs the "Real-World Accuracy" table in
# docs/benchmarks.md. The same numbers should reproduce here, modulo
# upstream commits since the last sweep.
#
# Usage:
#     scripts/dogfood_sweep.sh                # default: /tmp/shingan-dogfood
#     OUT_DIR=/path/to/dir scripts/dogfood_sweep.sh
#     SHINGAN=/path/to/shingan scripts/dogfood_sweep.sh
#
# Env:
#     OUT_DIR     where to clone repos + write reports (default /tmp/shingan-dogfood)
#     SHINGAN     shingan binary to use (default: `shingan` on PATH)
#     MIN_CONF    --min-confidence threshold (default 0.7)

set -euo pipefail

OUT_DIR="${OUT_DIR:-/tmp/shingan-dogfood}"
SHINGAN="${SHINGAN:-shingan}"
MIN_CONF="${MIN_CONF:-0.7}"

if ! command -v "$SHINGAN" >/dev/null 2>&1 && [ ! -x "$SHINGAN" ]; then
  echo "error: shingan binary not found on PATH (set SHINGAN=/path/to/shingan)" >&2
  exit 2
fi

mkdir -p "$OUT_DIR"
echo "Output directory: $OUT_DIR"
echo "Using binary:     $($SHINGAN version 2>/dev/null || echo "$SHINGAN")"
echo

# Corpus: <slug> <framework> <git url>
#   slug must be filesystem-safe; framework matches `shingan analyze --format`.
#   Listed in the same order as docs/benchmarks.md so output diffs neatly.
CORPUS=(
  "gpt-researcher          langgraph https://github.com/assafelovic/gpt-researcher.git"
  "open-deep-research      langgraph https://github.com/langchain-ai/open_deep_research.git"
  "executive-ai-assistant  langgraph https://github.com/langchain-ai/executive-ai-assistant.git"
  "company-researcher      langgraph https://github.com/langchain-ai/company-researcher.git"
  "data-enrichment         langgraph https://github.com/langchain-ai/data-enrichment.git"
  "datagen                 langgraph https://github.com/starpig1129/AI-Data-Analysis-MultiAgent.git"
  "devyan                  crewai    https://github.com/theyashwanthsai/Devyan.git"
  "swe-agent-langtalks     langgraph https://github.com/langtalks/swe-agent.git"
  "sragent                 langgraph https://github.com/ArcInstitute/SRAgent.git"
  "open-multi-agent-canvas langgraph https://github.com/CopilotKit/open-multi-agent-canvas.git"
  "letta                   langgraph https://github.com/letta-ai/letta.git"
  "langgraph-supervisor    langgraph https://github.com/langchain-ai/langgraph-supervisor-py.git"
  # ADK-Go (added 2026-05-11 after the corpus blind-spot called out).
  # adk-samples is the canonical Google ADK-Go sample collection;
  # each agent under go/agents/ is a separate workflow, so we sweep
  # those subdirectories rather than the repo root.
  "adk-samples-boat-agent          adk-go https://github.com/google/adk-samples.git#go/agents/boat-agent"
  "adk-samples-financial-advisor   adk-go https://github.com/google/adk-samples.git#go/agents/financial-advisor"
  "adk-samples-llm-auditor         adk-go https://github.com/google/adk-samples.git#go/agents/llm-auditor"
  "adk-samples-navallist           adk-go https://github.com/google/adk-samples.git#go/agents/navallist"
  "adk-samples-sail-researcher     adk-go https://github.com/google/adk-samples.git#go/agents/sail-researcher"
)

INDEX="$OUT_DIR/INDEX.md"
printf "# Shingan dogfood sweep\n\nGenerated: %s\nBinary:    %s\n\n" \
  "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  "$($SHINGAN version 2>/dev/null || echo "$SHINGAN")" > "$INDEX"
printf "| Repo | Framework | Findings | Critical | Report |\n|---|---|---|---|---|\n" >> "$INDEX"

for row in "${CORPUS[@]}"; do
  # shellcheck disable=SC2086
  set -- $row
  slug="$1"; framework="$2"; raw_url="$3"
  # Support `url#subdir` so multiple slugs can target different
  # subdirectories of the same upstream repo (e.g. adk-samples
  # hosts 5 distinct agent workflows under go/agents/*).
  url="${raw_url%%#*}"
  subdir=""
  if [ "$raw_url" != "$url" ]; then
    subdir="${raw_url#*#}"
  fi
  # Use the repo URL's basename for the local clone dir so multiple
  # slugs targeting the same repo share one checkout.
  clone_key=$(basename "$url" .git)
  repo_dir="$OUT_DIR/$clone_key"
  analyze_dir="$repo_dir"
  if [ -n "$subdir" ]; then
    analyze_dir="$repo_dir/$subdir"
  fi
  report="$OUT_DIR/$slug.report.md"

  if [ ! -d "$repo_dir/.git" ]; then
    echo "→ clone $clone_key (for $slug)"
    rm -rf "$repo_dir"
    git clone --depth=1 --quiet "$url" "$repo_dir" || {
      echo "  ! clone failed for $slug — skipping"
      printf "| %s | %s | _clone failed_ | — | — |\n" "$slug" "$framework" >> "$INDEX"
      continue
    }
  else
    echo "→ reuse $clone_key (for $slug)"
  fi

  if [ ! -d "$analyze_dir" ]; then
    echo "  ! analyze dir $analyze_dir missing — skipping"
    printf "| %s | %s | _subdir missing_ | — | — |\n" "$slug" "$framework" >> "$INDEX"
    continue
  fi

  echo "→ analyze $slug ($framework)"
  if ! "$SHINGAN" analyze \
       --format="$framework" \
       --input="$analyze_dir" \
       --output=markdown \
       --min-confidence="$MIN_CONF" \
       > "$report" 2>/dev/null; then
    : # exit code 1/2 just means findings exist; that's fine.
  fi

  # Extract the summary row written by shingan markdown reporter.
  summary=$(grep -A1 "| Total | Critical | Warning | Info |" "$report" 2>/dev/null | tail -1 || true)
  if [ -n "$summary" ]; then
    total=$(echo "$summary"   | awk -F'|' '{gsub(/ /,"",$2); print $2}')
    critical=$(echo "$summary"| awk -F'|' '{gsub(/ /,"",$3); print $3}')
  else
    total="?"; critical="?"
  fi

  printf "| [%s](%s) | %s | %s | %s | [report](./%s.report.md) |\n" \
    "$slug" "$url" "$framework" "$total" "$critical" "$slug" >> "$INDEX"
done

echo
echo "Sweep complete. Open $INDEX for the summary."
