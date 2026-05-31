"""Bare-Event fixture: steps annotated with the base ``Event`` type carry no
routable type information, so the parser must OMIT edges rather than invent
them (the spec's omit-don't-invent contract).

If the shim matched on bare ``Event``, it would cross-link every step and
fabricate a `b -> b` self-loop, which cycle_detection would then false-Critical.
The correct behaviour: `a`/`b`/`c` are nodes, but NO inter-step edges exist
(only the StopEvent on `c` is recognised, as a has_exit_branch sentinel).
"""
from __future__ import annotations

from llama_index.core.workflow import (
    Event,
    StartEvent,
    StopEvent,
    Workflow,
    step,
)


class BareEventFlow(Workflow):
    @step
    async def a(self, ev: StartEvent) -> Event:
        return Event()

    @step
    async def b(self, ev: Event) -> Event:
        return Event()

    @step
    async def c(self, ev: Event) -> StopEvent:
        return StopEvent(result="done")
