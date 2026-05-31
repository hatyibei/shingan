"""Linear LlamaIndex workflow: StartEvent -> MidEvent -> StopEvent.

Edges are derived by matching event types across steps:
  retrieve consumes StartEvent, returns MidEvent  → entry node
  synth    consumes MidEvent,   returns StopEvent  → exit node (has_exit_branch)

StartEvent / StopEvent are sentinels — never materialised as nodes. The edge
retrieve -> synth comes from MidEvent being produced by retrieve and consumed
by synth.
"""
from __future__ import annotations

from llama_index.core.workflow import (
    Event,
    StartEvent,
    StopEvent,
    Workflow,
    step,
)


class MidEvent(Event):
    payload: str


class LinearFlow(Workflow):
    @step
    async def retrieve(self, ev: StartEvent) -> MidEvent:
        return MidEvent(payload="hello")

    @step
    async def synth(self, ev: MidEvent) -> StopEvent:
        return StopEvent(result=ev.payload)
