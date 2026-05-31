"""Branching pydantic-graph workflow: Classify -> (HandleA | HandleB) -> End.

The entry node's `run` returns a union of two BaseNode subclasses, producing
two edges. Both handlers return End, so each is an exit node.
"""
from __future__ import annotations

from dataclasses import dataclass

from pydantic_graph import BaseNode, End, Graph, GraphRunContext


@dataclass
class Classify(BaseNode[str]):
    async def run(self, ctx: GraphRunContext) -> HandleA | HandleB:
        return HandleA()


@dataclass
class HandleA(BaseNode[str]):
    async def run(self, ctx: GraphRunContext) -> End[str]:
        return End("a")


@dataclass
class HandleB(BaseNode[str]):
    async def run(self, ctx: GraphRunContext) -> End[str]:
        return End("b")


graph = Graph(nodes=[Classify, HandleA, HandleB])
