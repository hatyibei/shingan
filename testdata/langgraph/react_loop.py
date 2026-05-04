"""ReAct-style agent — model ↔ tools loop with a termination decision.

Topology:
    START → model → (should_continue)
                       ├── "tools" → tools → model  (loopback, **unguarded**)
                       └── "end"   → END

This fixture is deliberately *unsafe*: the model→tools→model loop has no
explicit iteration guard. Shingan's `cycle_detection` rule should fire as
Critical, and `loop_guard` should also flag the missing max-iterations
parameter once we run analysis on the parsed graph.

The conditional mapping `{"tools": "tools", "end": END}` exercises Phase 1's
over-approximation: both edges are surfaced statically.
"""
from typing import Annotated, Sequence, TypedDict

from langgraph.graph import StateGraph, START, END


class AgentState(TypedDict):
    messages: Annotated[Sequence[dict], "scratchpad"]
    iteration: int


def call_model(state: AgentState) -> AgentState:
    """Invoke the LLM (mocked)."""
    return {
        "messages": list(state["messages"]) + [{"role": "assistant", "content": "..."}],
        "iteration": state["iteration"] + 1,
    }


def call_tools(state: AgentState) -> AgentState:
    """Invoke the requested tools (mocked: HTTP fetch)."""
    return {
        "messages": list(state["messages"]) + [{"role": "tool", "content": "tool_result"}],
        "iteration": state["iteration"],
    }


def should_continue(state: AgentState) -> str:
    """Branch on whether the model wants more tool calls."""
    last = state["messages"][-1]
    if last.get("role") == "tool":
        return "tools"
    return "end"


builder = StateGraph(AgentState)
builder.add_node("model", call_model)
builder.add_node("tools", call_tools)
builder.add_edge(START, "model")
builder.add_conditional_edges(
    "model",
    should_continue,
    {"tools": "tools", "end": END},
)
builder.add_edge("tools", "model")  # loopback — no guard.

graph = builder.compile()
