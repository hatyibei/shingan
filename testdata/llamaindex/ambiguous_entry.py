"""Ambiguous-entry LlamaIndex workflow: TWO steps consume StartEvent.

When more than one step consumes StartEvent, the entry is genuinely ambiguous
(LlamaIndex would dispatch StartEvent to all such steps). The shim leaves
entry_node_id empty and flags entry_ambiguous=True so reachability SKIPS the
graph rather than picking one and reporting the other as unreachable.

  ingest_a consumes StartEvent, returns StopEvent
  ingest_b consumes StartEvent, returns StopEvent
"""
from __future__ import annotations

from llama_index.core.workflow import (
    StartEvent,
    StopEvent,
    Workflow,
    step,
)


class FanInFlow(Workflow):
    @step
    async def ingest_a(self, ev: StartEvent) -> StopEvent:
        return StopEvent(result="a")

    @step
    async def ingest_b(self, ev: StartEvent) -> StopEvent:
        return StopEvent(result="b")
