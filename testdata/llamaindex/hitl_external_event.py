# Wild-shape fixture (botextractai/ai-event-driven -> main.py): a human-in-the-
# loop (HITL) step consumes a ``HumanResponseEvent`` that is injected EXTERNALLY
# by the run() driver via ``handler.ctx.send_event(HumanResponseEvent(...))`` —
# NO ``@step`` ever produces it. Before the 2026-06-05 fix the shim built edges
# only from producer->consumer event matching, found no producer for
# HumanResponseEvent, and flagged the consuming step (``get_feedback``) a false
# ``unreachable_node`` — even though it is reachable at runtime once the driver
# injects the event.
#
# After the fix HumanResponseEvent / InputRequiredEvent (and direct subclasses)
# are recognised as externally injectable: a step consuming one with no real
# in-edge is wired from the ENTRY node (modelling the external producer), so the
# entry still resolves unambiguously (``generate``) and ``get_feedback`` is NOT
# unreachable. ``request`` returning ``InputRequiredEvent`` is left
# produced-but-unconsumed (no edge, not an exit) — InputRequiredEvent flows OUT
# to the driver, not to another @step.
from __future__ import annotations

from llama_index.core.workflow import (
    Context,
    Event,
    HumanResponseEvent,
    InputRequiredEvent,
    StartEvent,
    StopEvent,
    Workflow,
    step,
)


class DraftEvent(Event):
    draft: str


class HITLApprovalFlow(Workflow):
    # Entry: consumes StartEvent, produces a normal DraftEvent.
    @step
    async def generate(self, ctx: Context, ev: StartEvent) -> DraftEvent:
        return DraftEvent(draft="proposed answer")

    # Emits InputRequiredEvent OUT to the run() driver (asking the human).
    # Nothing static consumes it — produced-but-unconsumed, not an exit.
    @step
    async def request(self, ctx: Context, ev: DraftEvent) -> InputRequiredEvent:
        return InputRequiredEvent(prefix="Approve this draft?")

    # Consumes the HumanResponseEvent the driver injects externally; no @step
    # produces it. Reachable at runtime — must NOT be flagged unreachable.
    @step
    async def get_feedback(self, ctx: Context, ev: HumanResponseEvent) -> StopEvent:
        return StopEvent(result=ev.response)
