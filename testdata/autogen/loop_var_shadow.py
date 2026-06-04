# `agent` is a for-loop control variable in a helper that does NOT add_node it,
# and is ALSO the name of a real builder node added elsewhere via the bare-name
# fallback. The real node must survive — a module-wide loop-target set would
# wrongly drop it. Regression guard for codex review #36.
from autogen_agentchat.teams import DiGraphBuilder


def _log(items):
    for agent in items:   # loop placeholder, never add_node'd
        print(agent)


builder = DiGraphBuilder()
agent = build_agent()          # custom factory, not a known agent ctor
helper = build_agent()
builder.add_node(agent)        # real node, resolved by bare-name fallback
builder.add_node(helper)
builder.add_edge(agent, helper)
builder.set_entry_point(agent)
