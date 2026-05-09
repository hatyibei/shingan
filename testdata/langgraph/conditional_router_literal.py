"""LangGraph `add_conditional_edges("src", router_fn)` (mapping omitted).

The third positional / `path_map` keyword can be omitted entirely;
LangGraph then introspects `router_fn`'s `-> Literal[...]` return-type
annotation at compile-time to discover the destinations.

Real-world dogfood: executive-ai-assistant/eaia/main/graph.py uses
this idiom on multiple routers (`route_after_triage`, `take_action`,
`enter_after_human`). Each unmapped conditional was responsible for
several apparent `unreachable_node` warnings because the static
graph never picked up the branches the router could choose.
"""

from typing import Literal
from langgraph.graph import StateGraph, START, END


def triage(state):
    return state


def draft(state):
    return state


def archive(state):
    return state


def notify(state):
    return state


def route_after_triage(
    state,
) -> Literal["draft", "archive", "notify"]:
    """Router function — its return-type Literal enumerates every
    possible next node. LangGraph reads this at compile-time so a
    mapping argument to `add_conditional_edges` is unnecessary."""
    return "draft"


builder = StateGraph(dict)
builder.add_node("triage", triage)
builder.add_node("draft", draft)
builder.add_node("archive", archive)
builder.add_node("notify", notify)
builder.add_edge(START, "triage")
# Note: NO third arg — LangGraph picks dests from route_after_triage's
# return-type annotation. Without router_literal extraction,
# {draft, archive, notify} all look unreachable.
builder.add_conditional_edges("triage", route_after_triage)
builder.add_edge("draft", END)
builder.add_edge("archive", END)
builder.add_edge("notify", END)

graph = builder.compile()
