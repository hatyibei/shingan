"""Conditional routing — `add_conditional_edges` with a static mapping.

Demonstrates Shingan's over-approximation handling: the runtime decision is
made by `route()`, but every key in `path_map` becomes an edge candidate at
parse time. ConfidenceReason will be `over_approximated_dynamic` once Track R
lands.

Topology:
    START → triage → (route) → answer | escalate | refund → END

Expected Shingan findings:
    - none today (Phase 1 only emits the candidate edges).
"""
from typing import TypedDict

from langgraph.graph import StateGraph, START, END


class State(TypedDict):
    user_input: str
    intent: str
    answer: str


def triage(state: State) -> State:
    return {**state, "intent": classify_intent(state["user_input"])}


def classify_intent(text: str) -> str:
    # Stand-in for an LLM-backed classifier.
    if "refund" in text:
        return "refund"
    if "agent" in text or "human" in text:
        return "escalate"
    return "answer"


def answer(state: State) -> State:
    return {**state, "answer": f"FAQ: {state['user_input']}"}


def escalate(state: State) -> State:
    return {**state, "answer": "Connecting you with a human agent."}


def refund(state: State) -> State:
    return {**state, "answer": "Refund flow initialised."}


def route(state: State) -> str:
    return state["intent"]


builder = StateGraph(State)
builder.add_node("triage", triage)
builder.add_node("answer", answer)
builder.add_node("escalate", escalate)
builder.add_node("refund", refund)
builder.add_edge(START, "triage")
builder.add_conditional_edges(
    "triage",
    route,
    {"answer": "answer", "escalate": "escalate", "refund": "refund"},
)
builder.add_edge("answer", END)
builder.add_edge("escalate", END)
builder.add_edge("refund", END)

graph = builder.compile()
