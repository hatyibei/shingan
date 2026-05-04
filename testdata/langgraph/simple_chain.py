"""Sequential agent — START → classify → respond → END.

Three-node straight chain. Used as the baseline LangGraph test fixture:
no conditional edges, no loops, every node deterministically reachable.

Expected Shingan analysis: zero findings (graph is healthy).
"""
from typing import TypedDict

from langgraph.graph import StateGraph, START, END


class State(TypedDict):
    """Conversation state passed between nodes."""

    user_input: str
    classification: str
    response: str


def classify(state: State) -> State:
    """Classify the user's intent (mocked for fixture purposes)."""
    return {**state, "classification": "greeting"}


def respond(state: State) -> State:
    """Produce the assistant's reply based on the classification."""
    return {**state, "response": f"Hello, {state['user_input']}!"}


builder = StateGraph(State)
builder.add_node("classify", classify)
builder.add_node("respond", respond)
builder.add_edge(START, "classify")
builder.add_edge("classify", "respond")
builder.add_edge("respond", END)

graph = builder.compile()
