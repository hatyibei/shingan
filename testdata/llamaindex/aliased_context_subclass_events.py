# Wild-shape fixture (zylon-ai/private-gpt → image_handler.py): two things that
# defeated the leaf-name matching before the 2026-06-03 fix —
#
#   1. The Context param is annotated with a PROJECT-LOCAL ALIAS, `ctx:
#      AnyContext` (where `AnyContext = Context` elsewhere), NOT the literal
#      `Context`. The old shim skipped the context param only when the
#      annotation leaf was literally `Context`, so it mis-read `AnyContext` as
#      the consumed event → every step consumed `AnyContext`, no event types
#      matched → 0 edges, entry_ambiguous, no exit branch (false "looks clean").
#
#   2. The entry/exit events are user-defined direct SUBCLASSES of the
#      sentinels — `class InputEvent(StartEvent)` / `class ResultEvent(StopEvent)`
#      — and steps annotate the subclass, not the bare sentinel. The old shim
#      matched StartEvent/StopEvent by literal leaf name only, so the entry and
#      has_exit_branch never resolved even once the ctx alias was handled.
#
# After the fix the graph recovers: begin (consumes InputEvent<:StartEvent) is
# the entry, the middle->finish edge is matched by MidEvent, and finish (returns
# ResultEvent<:StopEvent) carries has_exit_branch.
from __future__ import annotations

from llama_index.core.workflow import (
    Context,
    Event,
    StartEvent,
    StopEvent,
    Workflow,
    step,
)

# Project-local Context alias, exactly like private-gpt's `AnyContext`.
AnyContext = Context


class InputEvent(StartEvent):
    query: str


class MidEvent(Event):
    payload: str


class ResultEvent(StopEvent):
    answer: str


class AliasedContextFlow(Workflow):
    @step
    async def begin(self, ctx: AnyContext, ev: InputEvent) -> MidEvent:
        return MidEvent(payload=ev.query)

    @step
    async def finish(self, ctx: AnyContext, ev: MidEvent) -> ResultEvent:
        return ResultEvent(answer=ev.payload)
