"""Two StateGraphs in one module — subgraph composition pattern.

Modeled on `open_deep_research/src/legacy/graph.py` (real-world dogfood
target). The earlier flat-merge AST visitor combined nodes from both
graphs under a single entry, generating false-positive `unreachable_node`
warnings for every node of the outer graph.

Expected behavior: the visitor returns the **outer** graph (the one
named `graph` after `builder.compile()`), not the inner subgraph
(`section_builder`).
"""

from langgraph.graph import StateGraph, START, END


def gen_query(state):
    return state


def web_search(state):
    return state


def write_section(state):
    return state


def plan(state):
    return state


def section(state):
    return state


def gather(state):
    return state


def finalize(state):
    return state


# Inner subgraph (small).
section_builder = StateGraph(dict)
section_builder.add_node("gen_query", gen_query)
section_builder.add_node("web_search", web_search)
section_builder.add_node("write_section", write_section)
section_builder.add_edge(START, "gen_query")
section_builder.add_edge("gen_query", "web_search")
section_builder.add_edge("web_search", "write_section")

# Outer (production) graph — composes the subgraph as one of its nodes.
builder = StateGraph(dict)
builder.add_node("plan", plan)
builder.add_node("section", section_builder.compile())
builder.add_node("gather", gather)
builder.add_node("finalize", finalize)
builder.add_edge(START, "plan")
builder.add_edge("plan", "section")
builder.add_edge("section", "gather")
builder.add_edge("gather", "finalize")
builder.add_edge("finalize", END)

graph = builder.compile()
