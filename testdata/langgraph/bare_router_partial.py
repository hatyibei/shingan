"""Untyped multi-return router used via `a or functools.partial(b, ...)`.

Modeled on langgraph/examples/chatbot-simulation-evaluation/
simulation_utils.py — `add_conditional_edges("user", should_continue
or functools.partial(_should_continue, max_turns=6))`. Two patterns
the visitor must handle simultaneously:

  1. Source 3 (bare-return router): `_should_continue` has no return-
     type annotation; multiple `return END` / `return "assistant"`
     statements identify it as a router via the ≥2-distinct-literals
     heuristic.
  2. BoolOp + functools.partial wrapper: `add_conditional_edges`'s
     2nd argument is `should_continue or functools.partial(_should_continue, …)`
     — the visitor recurses through `_resolve_router_callable` and
     pulls `_should_continue` out of the BoolOp / partial wrapper.

  3. Two-pass visit: `_should_continue` is defined AFTER
     `make_graph`, so without the prepass `_command_goto_map` is
     empty when `add_conditional_edges` is visited.
"""

import functools
from langgraph.graph import StateGraph, START, END


def make_graph():
    builder = StateGraph(dict)
    builder.add_node("user", lambda s: s)
    builder.add_node("assistant", lambda s: s)
    builder.add_edge(START, "user")
    builder.add_edge("assistant", "user")
    builder.add_conditional_edges(
        "user",
        functools.partial(_should_continue, max_turns=6),
    )
    return builder.compile()


# Defined AFTER make_graph — exercises the visitor's two-pass design.
def _should_continue(state, max_turns: int = 6):
    if state.get("turns", 0) > max_turns:
        return END
    if state.get("done"):
        return END
    return "assistant"


graph = make_graph()
