#!/usr/bin/env bash
#
# check_confidence_reason.sh — fail when a domain.Finding{...} literal in a
# rule omits the ConfidenceReason field. ADR-008 requires every Finding to
# carry an explicit Reason so --min-confidence filtering stays interpretable.
# Pure Go cannot mark a struct field as required at compile time, so we lint
# for it.
#
# Usage:
#   ./scripts/check_confidence_reason.sh                # scan domain/rules/*.go
#   ./scripts/check_confidence_reason.sh path/to/dir    # scan a custom dir
#
# Exit codes:
#   0 — every populated Finding literal has a ConfidenceReason
#   1 — at least one literal is missing the field; offending sites printed
#
# What is checked:
#   - domain.Finding{ ... } literals where the brace block contains at least
#     one field assignment ("Foo: bar"). The empty sentinel domain.Finding{}
#     used as "no finding" return is excluded automatically.
set -euo pipefail

target_dir="${1:-domain/rules}"

if [[ ! -d "$target_dir" ]]; then
  echo "check_confidence_reason: directory not found: $target_dir" >&2
  exit 1
fi

mapfile -t files < <(find "$target_dir" -type f -name '*.go' ! -name '*_test.go' | sort)

if [[ ${#files[@]} -eq 0 ]]; then
  echo "check_confidence_reason: no .go files under $target_dir" >&2
  exit 0
fi

total_violations=0
for file in "${files[@]}"; do
  # awk state machine: capture each `domain.Finding{ ... }` block, then check
  # whether it contains any field assignment AND no ConfidenceReason field.
  output=$(awk -v f="$file" '
    function emit_block(    fields_present) {
      if (block == "") return
      # Skip empty-literal sentinel: no `:` field assignment inside.
      if (block !~ /:/) return
      if (block !~ /ConfidenceReason:/) {
        printf("%s:%d: domain.Finding literal missing ConfidenceReason\n", f, start)
        n = split(block, lines, "\n")
        for (i = 1; i <= n && lines[i] != ""; i++) printf("    %s\n", lines[i])
        violations++
      }
    }
    {
      line = $0
      while (length(line) > 0) {
        if (!in_block) {
          # Look for the start of a Finding literal on this line.
          where = match(line, /domain\.Finding\{/)
          if (where == 0) break
          in_block = 1
          start = NR
          # Reset depth based on the just-opened brace; consume that "{".
          depth = 1
          block = substr(line, where)
          line = substr(line, where + RLENGTH)
          # Continue scanning the rest of the line for matching close.
          tail = ""
          for (i = 1; i <= length(line); i++) {
            ch = substr(line, i, 1)
            tail = tail ch
            if (ch == "{") depth++
            else if (ch == "}") {
              depth--
              if (depth == 0) {
                block = block tail
                emit_block()
                in_block = 0
                block = ""
                line = substr(line, i + 1)
                break
              }
            }
          }
          if (in_block) {
            block = block line "\n"
            line = ""
          }
        } else {
          tail = ""
          for (i = 1; i <= length(line); i++) {
            ch = substr(line, i, 1)
            tail = tail ch
            if (ch == "{") depth++
            else if (ch == "}") {
              depth--
              if (depth == 0) {
                block = block tail
                emit_block()
                in_block = 0
                block = ""
                line = substr(line, i + 1)
                break
              }
            }
          }
          if (in_block) {
            block = block line "\n"
            line = ""
          }
        }
      }
    }
    END { exit (violations > 0) ? 1 : 0 }
  ' "$file") || rc=$? && rc=${rc:-0}

  if [[ -n "$output" ]]; then
    echo "$output"
    total_violations=$((total_violations + 1))
  fi
  unset rc
done

if [[ $total_violations -gt 0 ]]; then
  echo "check_confidence_reason: $total_violations file(s) contain offending Finding literals" >&2
  exit 1
fi

echo "check_confidence_reason: OK (${#files[@]} files scanned)"
