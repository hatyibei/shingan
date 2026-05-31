"""Branching LlamaIndex workflow: a step returns a union of two events.

  classify consumes StartEvent, returns AEvent | BEvent  → entry node
  handle_a consumes AEvent,     returns StopEvent
  handle_b consumes BEvent,     returns StopEvent

The union return on `classify` fans out: AEvent is consumed by handle_a and
BEvent by handle_b, so we get edges classify -> handle_a and classify ->
handle_b. Both handlers return StopEvent, so each carries has_exit_branch.
Also exercises a `Context` parameter that must be skipped when locating the
consumed event.
"""
from __future__ import annotations

from llama_index.core.workflow import (
    Context,
    Event,
    StartEvent,
    StopEvent,
    Workflow,
    step,
)


class AEvent(Event):
    pass


class BEvent(Event):
    pass


class BranchingFlow(Workflow):
    @step
    async def classify(self, ctx: Context, ev: StartEvent) -> AEvent | BEvent:
        if ev.get("kind") == "a":
            return AEvent()
        return BEvent()

    @step
    async def handle_a(self, ev: AEvent) -> StopEvent:
        return StopEvent(result="a")

    @step
    async def handle_b(self, ev: BEvent) -> StopEvent:
        return StopEvent(result="b")
