# Two SEPARATE Graph(nodes=[...]) declarations in one module: a parent graph
# (Plan/Feedback/Finalize/Report) plus a subgraph (SubEntry/SubSearch/SubWrite)
# whose entry is invoked from a parent node via `section_graph.run(SubEntry())`.
# Mirrors the wild dogfood 2026-06-03 shape (X-Zero-L/pydantic-ai-deep-research
# graph.py: `section_graph` + `graph`).
#
# Only SubEntry is zero-in-degree (Plan has an in-edge from Feedback's
# `return Plan()` back-branch), so a single merged graph would pick SubEntry as
# THE entry and report the four parent nodes as unreachable false positives.
# With >=2 distinct Graph declarations each contributing a known node, the
# parser treats the entry as AMBIGUOUS (entry unset) — and that ambiguity wins
# over the explicit `section_graph.run(SubEntry())` start — so reachability
# SKIPS the merged graph. Asserts REACHABILITY only (cycle_detection on this
# shape is a separate, deferred concern — do NOT assert it here).
from typing import Union
from pydantic_graph import BaseNode, End, Graph


# ---- parent graph ----------------------------------------------------------
class Plan(BaseNode):
    async def run(self, ctx) -> "Feedback":
        return Feedback()


class Feedback(BaseNode):
    async def run(self, ctx) -> Union["Plan", "Finalize"]:
        if False:
            return Plan()  # back-edge: Plan is NOT zero-in-degree
        # Drive the subgraph from a parent node — the canonical sub-graph idiom.
        await section_graph.run(SubEntry())
        return Finalize()


class Finalize(BaseNode):
    async def run(self, ctx) -> "Report":
        return Report()


class Report(BaseNode):
    async def run(self, ctx) -> End:
        return End(None)


# ---- subgraph --------------------------------------------------------------
class SubEntry(BaseNode):
    async def run(self, ctx) -> "SubSearch":
        return SubSearch()


class SubSearch(BaseNode):
    async def run(self, ctx) -> "SubWrite":
        return SubWrite()


class SubWrite(BaseNode):
    async def run(self, ctx) -> Union["SubSearch", End]:
        if ctx.state.done:
            return End(None)
        return SubSearch()


section_graph = Graph(
    nodes=[
        SubEntry,
        SubSearch,
        SubWrite,
    ],
)

graph = Graph(
    nodes=[
        Plan,
        Feedback,
        Finalize,
        Report,
    ],
)
