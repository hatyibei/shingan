# Two BaseNode roots with no explicit start. pydantic-graph is runnable from
# any node, so the entry is genuinely AMBIGUOUS — reachability must SKIP the
# graph rather than pick one root and report the other as unreachable
# (codex review 2026-05-31). Neither A nor B is a destination of the other.
from pydantic_graph import BaseNode, End, Graph


class A(BaseNode):
    async def run(self, ctx) -> End[int]:
        return End(1)


class B(BaseNode):
    async def run(self, ctx) -> End[int]:
        return End(2)


graph = Graph(nodes=[A, B])
