# Keyword-only event parameter (`async def run(self, *, ev: StartEvent)`) must
# still be located — codex review 2026-05-31 found kwonly events were skipped,
# making the entry ambiguous and dropping event-derived edges.
from llama_index.core.workflow import Workflow, step, StartEvent, StopEvent


class Flow(Workflow):
    @step
    async def run(self, *, ev: StartEvent) -> StopEvent:
        return StopEvent(result="ok")
