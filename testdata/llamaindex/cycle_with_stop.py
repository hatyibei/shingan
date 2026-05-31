"""Cycle-with-StopEvent acceptance fixture (mirrors the pydantic-graph
cycle_with_end + langgraph-js ReactLoop tests).

A draft/critique/revise loop that can iterate but also exit:

  setup:    StartEvent  -> DraftEvent                     (entry)
  critique: DraftEvent  -> ReviseEvent | StopEvent        (loop OR exit)
  revise:   ReviseEvent -> DraftEvent                     (back-edge)

Event matching yields edges:
  setup    -> critique   (DraftEvent)
  critique -> revise     (ReviseEvent)
  revise   -> critique   (DraftEvent)          ← back-edge closes the cycle

The strongly-connected component is {critique, revise}. `critique` returns a
union including StopEvent, so the shim sets has_exit_branch=True on `critique`
— which is IN the cycle. cycle_detection must therefore downgrade the cycle
from Critical to Warning (has_exit_branch parity).

Deliberately union-free on the consumer side: every step consumes a single
event type, so the back-edge does not depend on consumer-side union flattening.
"""
from __future__ import annotations

from llama_index.core.workflow import (
    Event,
    StartEvent,
    StopEvent,
    Workflow,
    step,
)


class DraftEvent(Event):
    text: str


class ReviseEvent(Event):
    text: str


class RefineFlow(Workflow):
    @step
    async def setup(self, ev: StartEvent) -> DraftEvent:
        return DraftEvent(text=ev.get("prompt", ""))

    @step
    async def critique(self, ev: DraftEvent) -> ReviseEvent | StopEvent:
        if self._good_enough(ev.text):
            return StopEvent(result=ev.text)
        return ReviseEvent(text=ev.text)

    @step
    async def revise(self, ev: ReviseEvent) -> DraftEvent:
        return DraftEvent(text=ev.text + "!")

    def _good_enough(self, text: str) -> bool:
        return len(text) > 100
