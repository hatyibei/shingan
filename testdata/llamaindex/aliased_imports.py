# Aliased framework imports (Workflow as WF, step as li_step, StartEvent as SE,
# StopEvent as XE) must still be recognised — codex review 2026-05-31 found
# aliased source silently produced an empty graph. MidEvent is a non-framework
# event (not aliased) so the begin->finish edge is matched by its own name.
from llama_index.core.workflow import Workflow as WF, step as li_step, StartEvent as SE, StopEvent as XE
from .events import MidEvent


class Flow(WF):
    @li_step
    async def begin(self, ev: SE) -> MidEvent:
        return MidEvent()

    @li_step
    async def finish(self, ev: MidEvent) -> XE:
        return XE(result="done")
