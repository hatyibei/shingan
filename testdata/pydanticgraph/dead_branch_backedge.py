# Issue #40 (X-Zero-L/pydantic-ai-deep-research HumanFeedback.run shape).
# HumanFeedback's annotation advertises a back-edge to ReportPlanner, but the
# only body return of ReportPlanner is gated by `if False:` — statically dead.
# The parser must NOT draw HumanFeedback -> ReportPlanner, so no impossible
# cycle is reported. Writer's annotation-only leaf `Reviewer` (no body return
# at all, returned via a variable) must still be kept.
from typing import Union

from pydantic_graph import BaseNode, End, Graph


class ReportPlanner(BaseNode):
    async def run(self, ctx) -> "HumanFeedback":
        return HumanFeedback()


class HumanFeedback(BaseNode):
    async def run(self, ctx) -> Union["ReportPlanner", "Writer"]:
        if False:
            return ReportPlanner()
        return Writer()


class Writer(BaseNode):
    async def run(self, ctx) -> Union["Reviewer", End[str]]:
        nxt = Reviewer()
        if ctx.state.done:
            return End("ok")
        return nxt


class Reviewer(BaseNode):
    async def run(self, ctx) -> End[str]:
        if True:
            return End("done")
        else:
            return Writer()


graph = Graph(nodes=[ReportPlanner, HumanFeedback, Writer, Reviewer])
