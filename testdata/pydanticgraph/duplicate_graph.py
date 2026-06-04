# Two Graph(...) declarations naming the SAME node set (a graph exported twice /
# via an alias). This is ONE logical graph, so reachability MUST still run
# (entry resolved, not ambiguous) — counting declarations alone would wrongly
# suppress it. Regression guard for codex review #36.
from pydantic_graph import BaseNode, End, Graph


class Start(BaseNode):
    async def run(self, ctx) -> "Work":
        return Work()


class Work(BaseNode):
    async def run(self, ctx) -> End[int]:
        return End(0)


graph = Graph(nodes=[Start, Work])
graph_alias = Graph(nodes=[Start, Work])
