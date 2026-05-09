"""LangGraph cycle with explicit END sentinel exit branch.

Verifies the cross-layer "structural exit via sentinel" pipeline:

  Python shim recognises `add_edge(node, END)` (or
  `Literal[END, …]` router return) and emits node-level
  `has_exit_branch=true` in the JSON.

  Go parser deserialises that into `domain.Node.HasExitBranch`.

  `cycle_detection` reads the typed field via
  `cycleHasExit` and downgrades the cycle from Critical to Warning
  even when the only exit edge points at the (un-materialised)
  END sentinel.

Dogfood: the analogous structure in
langchain-ai/company-researcher's `route_from_reflection`.
"""

from langgraph.graph import StateGraph, START, END


def planner(state):
    return state


def reviewer(state):
    return state


builder = StateGraph(dict)
builder.add_node("planner", planner)
builder.add_node("reviewer", reviewer)
builder.add_edge(START, "planner")
builder.add_edge("planner", "reviewer")
# `reviewer` either loops back to planner or terminates via END —
# the only structural exit is the END branch. Without sentinel
# exit recognition the planner ↔ reviewer cycle would be classed
# as Critical "graph definition error".
builder.add_edge("reviewer", "planner")
builder.add_edge("reviewer", END)
graph = builder.compile()
