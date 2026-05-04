"""Retrieval-Augmented Generation pipeline — PII-leak fixture.

Topology:
    START → retrieve_docs → llm_summarise → external_webhook_post → END

Why this matters for Shingan:
- `retrieve_docs` is a `tool` node fetching from an internal knowledge base
  (potential PII source).
- `external_webhook_post` is a `tool` node that emits to a third-party HTTP
  endpoint (PII sink).
- There is **no** human-in-the-loop / approval node between the two.
- `pii_leak_scanner` should flag the unbroken path from RAG source to
  external sink with no Human gate.

The naming convention (`retrieve_*`, `*_webhook_post`) is what the existing
heuristics in domain/rules/pii_leak.go already recognise, so we keep the
node names aligned with those signals.
"""
from typing import TypedDict, List

from langgraph.graph import StateGraph, START, END


class RAGState(TypedDict):
    user_query: str
    knowledge_documents: List[str]
    summary: str


def retrieve_docs(state: RAGState) -> RAGState:
    """Knowledge-base retrieval (PII source)."""
    return {
        **state,
        "knowledge_documents": [
            "Customer email: alice@example.com — issue: refund request.",
            "Customer email: bob@example.com — issue: login problem.",
        ],
    }


def llm_summarise(state: RAGState) -> RAGState:
    """Summarise the retrieved docs (LLM)."""
    body = " ".join(state["knowledge_documents"])
    return {**state, "summary": body[:200]}


def external_webhook_post(state: RAGState) -> RAGState:
    """Forward summary to a third-party webhook (PII sink)."""
    # Real code would hit https://hooks.example.com/...
    return state


builder = StateGraph(RAGState)
builder.add_node("retrieve_docs", retrieve_docs)
builder.add_node("llm_summarise", llm_summarise)
builder.add_node("external_webhook_post", external_webhook_post)
builder.add_edge(START, "retrieve_docs")
builder.add_edge("retrieve_docs", "llm_summarise")
builder.add_edge("llm_summarise", "external_webhook_post")
builder.add_edge("external_webhook_post", END)

graph = builder.compile()
