"""Cycle-with-End acceptance fixture (mirrors the LangGraph.js ReactLoop test).

`Counter.run` returns `Self | End[int]`: a self-loop that exits via End. The
shim emits a self-edge (Counter -> Counter) AND sets has_exit_branch=True on
Counter (the End in the union). The cycle_detection rule must therefore
downgrade this cycle from Critical to Warning — exactly the has_exit_branch
parity the langgraph-js ReactLoop test locks in.

If the shim failed to map `Self` to a self-edge there would be no cycle at all
("expected a cycle finding, got none"); if it failed to set has_exit_branch the
cycle would false-Critical.
"""
from __future__ import annotations

from dataclasses import dataclass
from typing import Self

from pydantic_graph import BaseNode, End, Graph, GraphRunContext


@dataclass
class Counter(BaseNode[int]):
    async def run(self, ctx: GraphRunContext) -> Self | End[int]:
        if ctx.state >= 5:
            return End(ctx.state)
        return self


graph = Graph(nodes=[Counter])
