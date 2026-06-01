# ModeratorNode is annotated `-> ProNode` but its BODY returns ProNode,
# ConNode AND DecisionNode. Real pydantic-graph code frequently has incomplete
# return annotations (dogfood 2026-06-01: aidev9/tuts product-decision-graph).
# The parser must read the body returns too, so ConNode / DecisionNode are
# reachable and the Moderator<->Pro/Con cycle exits via DecisionNode -> End
# (a bounded-cycle Warning, NOT a false no-exit Critical + false unreachable).
from pydantic_graph import BaseNode, End, Graph


class ModeratorNode(BaseNode):
    async def run(self, ctx) -> "ProNode":  # incomplete: body returns 3 types
        if ctx.state.a:
            return ProNode()
        if ctx.state.b:
            return ConNode()
        return DecisionNode()


class ProNode(BaseNode):
    async def run(self, ctx) -> ModeratorNode:
        return ModeratorNode()


class ConNode(BaseNode):
    async def run(self, ctx) -> ModeratorNode:
        return ModeratorNode()


class DecisionNode(BaseNode):
    async def run(self, ctx) -> End[int]:
        return End(1)


graph = Graph(nodes=[ModeratorNode, ProNode, ConNode, DecisionNode])
