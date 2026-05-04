"""Supervisor + workers — full-graph fixture.

Topology:
    START → supervisor → (route) → researcher | coder | reviewer
    researcher / coder / reviewer → supervisor   (loopback)
    supervisor (terminal branch) → END

This exercises:
- `add_conditional_edges` with three workers (over-approximation).
- A loopback from each worker to the supervisor (cycle test material).
- A terminal branch from the supervisor to END.

The loop is bounded indirectly by the supervisor's "finish" decision, but
there is no explicit iteration cap — `loop_guard` heuristics may flag this.
"""
from typing import TypedDict

from langgraph.graph import StateGraph, START, END


class TeamState(TypedDict):
    task: str
    research_notes: str
    code: str
    review: str
    next_step: str


def supervisor(state: TeamState) -> TeamState:
    """Pick the next worker based on accumulated state."""
    if not state.get("research_notes"):
        nxt = "researcher"
    elif not state.get("code"):
        nxt = "coder"
    elif not state.get("review"):
        nxt = "reviewer"
    else:
        nxt = "finish"
    return {**state, "next_step": nxt}


def researcher(state: TeamState) -> TeamState:
    return {**state, "research_notes": f"Notes for: {state['task']}"}


def coder(state: TeamState) -> TeamState:
    return {**state, "code": "def solve(): ..."}


def reviewer(state: TeamState) -> TeamState:
    return {**state, "review": "LGTM"}


def route(state: TeamState) -> str:
    return state["next_step"]


builder = StateGraph(TeamState)
builder.add_node("supervisor", supervisor)
builder.add_node("researcher", researcher)
builder.add_node("coder", coder)
builder.add_node("reviewer", reviewer)
builder.add_edge(START, "supervisor")
builder.add_conditional_edges(
    "supervisor",
    route,
    {
        "researcher": "researcher",
        "coder": "coder",
        "reviewer": "reviewer",
        "finish": END,
    },
)
builder.add_edge("researcher", "supervisor")
builder.add_edge("coder", "supervisor")
builder.add_edge("reviewer", "supervisor")

graph = builder.compile()
