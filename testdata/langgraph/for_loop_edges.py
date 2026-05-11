"""Regression fixture for v0.8.7 FP-1: for-loop edge unrolling.

Minimal reproduction of starpig1129/DATAGEN (a.k.a.
AI-Data-Analysis-MultiAgent) workflow.py:119, where edges added in a
`for` loop iterating a literal list of node names were not unrolled by
the AST fallback. The result was two spurious `unreachable_node`
findings on `QualityReview` and `NoteTaker`.

Expected behaviour after the v0.8.7 fix: zero `unreachable_node`
findings on this graph.
"""

from typing import TypedDict
from langgraph.graph import END, START, StateGraph


class State(TypedDict):
    messages: list


def hypothesis_router(state: State) -> str:
    return "Process"


def process_router(state: State) -> str:
    return "Coder"


def quality_router(state: State) -> str:
    return "NoteTaker"


workflow = StateGraph(State)
workflow.add_node("Hypothesis", lambda s: s)
workflow.add_node("HumanChoice", lambda s: s)
workflow.add_node("Process", lambda s: s)
workflow.add_node("Visualization", lambda s: s)
workflow.add_node("Search", lambda s: s)
workflow.add_node("Coder", lambda s: s)
workflow.add_node("Report", lambda s: s)
workflow.add_node("QualityReview", lambda s: s)
workflow.add_node("NoteTaker", lambda s: s)

workflow.add_edge(START, "Hypothesis")
workflow.add_edge("Hypothesis", "HumanChoice")

workflow.add_conditional_edges(
    "HumanChoice",
    hypothesis_router,
    {"Hypothesis": "Hypothesis", "Process": "Process"},
)

workflow.add_conditional_edges(
    "Process",
    process_router,
    {
        "Coder": "Coder",
        "Search": "Search",
        "Visualization": "Visualization",
        "Report": "Report",
        "Process": "Process",
    },
)

# The pattern this fixture exercises:
for member in ["Visualization", "Search", "Coder", "Report"]:
    workflow.add_edge(member, "QualityReview")

workflow.add_conditional_edges(
    "QualityReview",
    quality_router,
    {
        "Visualization": "Visualization",
        "Search": "Search",
        "Coder": "Coder",
        "Report": "Report",
        "NoteTaker": "NoteTaker",
    },
)

workflow.add_edge("NoteTaker", END)

graph = workflow.compile()
