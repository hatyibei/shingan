"""Fan-in fixture: a step whose event param is a UNION consumes both event
types (consumer-side union flattening), so it gets an in-edge from each
producer.

  split   consumes StartEvent, returns LeftEvent | RightEvent  (entry)
  collect consumes LeftEvent | RightEvent, returns StopEvent

`split` produces both LeftEvent and RightEvent; `collect`'s union param consumes
both, so we get split -> collect from EACH event type (deduped to one edge).
"""
from __future__ import annotations

from llama_index.core.workflow import (
    Event,
    StartEvent,
    StopEvent,
    Workflow,
    step,
)


class LeftEvent(Event):
    pass


class RightEvent(Event):
    pass


class FanInFlow(Workflow):
    @step
    async def split(self, ev: StartEvent) -> LeftEvent | RightEvent:
        return LeftEvent()

    @step
    async def collect(self, ev: LeftEvent | RightEvent) -> StopEvent:
        return StopEvent(result="merged")
