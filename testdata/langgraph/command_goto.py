"""LangGraph Command(goto=...) implicit-edge pattern.

Two of the most common idioms LangGraph production agents use to
express dynamic next-step routing without `add_conditional_edges`:

  1. **Typed Command return** — `def fn(...) -> Command[Literal[...]]`
     The return-type annotation enumerates every reachable
     destination so static analysis can recover them without running
     the function. Used by open_deep_research/legacy/graph.py
     `human_feedback`.

  2. **Bare Command(goto=...) returns** — no return-type annotation,
     just `return Command(goto="x")` inside the function body. Used
     by older LangGraph examples and ad-hoc routing logic.

The shim is expected to extract destinations from BOTH patterns and
materialise them as `from=node_name → to=destination` edges with
`condition="command_goto"` in the WorkflowGraph.
"""

from typing import Literal
from langgraph.graph import StateGraph, START, END
from langgraph.types import Command


def planner(state):
    return state


def writer(state):
    return state


def reviewer(state):
    return state


def archiver(state):
    return state


def human_feedback(
    state,
) -> Command[Literal["planner", "writer"]]:
    """Typed Command — both 'planner' and 'writer' destinations
    are reachable from this node, but only via runtime branching.
    Without Command/goto extraction this node looks like a dead end."""
    if state.get("revise"):
        return Command(goto="planner")
    return Command(goto="writer")


def router(state):
    """Bare Command(goto=...) — no return-type annotation, the shim
    must scan the body for `return Command(goto="x")` literals."""
    if state.get("done"):
        return Command(goto="archiver")
    return Command(goto="reviewer")


builder = StateGraph(dict)
builder.add_node("planner", planner)
builder.add_node("writer", writer)
builder.add_node("human_feedback", human_feedback)
builder.add_node("reviewer", reviewer)
builder.add_node("archiver", archiver)
builder.add_node("router", router)
builder.add_edge(START, "planner")
builder.add_edge("planner", "human_feedback")
builder.add_edge("writer", "router")
builder.add_edge("reviewer", END)
builder.add_edge("archiver", END)

graph = builder.compile()
