"""Linear pydantic-graph workflow: A -> B -> End.

Edges are derived from each node's `run` return-type annotation. `B` returns
`End[int]`, so it is an exit node (has_exit_branch=True) and End is never a
materialised node.
"""
from __future__ import annotations

from dataclasses import dataclass

from pydantic_graph import BaseNode, End, Graph, GraphRunContext


@dataclass
class A(BaseNode[int]):
    async def run(self, ctx: GraphRunContext) -> B:
        return B()


@dataclass
class B(BaseNode[int]):
    async def run(self, ctx: GraphRunContext) -> End[int]:
        return End(42)


graph = Graph(nodes=[A, B])
