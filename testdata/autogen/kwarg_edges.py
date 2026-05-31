# DiGraphBuilder edges expressed with keyword arguments
# (add_edge(source=.., target=..), add_node(node=..)) must still be parsed
# (codex review 2026-05-31, P2): positional-only scanning dropped them.
from autogen_agentchat.teams import DiGraphBuilder
from autogen_agentchat.agents import AssistantAgent

planner = AssistantAgent(name="planner")
worker = AssistantAgent(name="worker")
reviewer = AssistantAgent(name="reviewer")

builder = DiGraphBuilder()
builder.add_node(node=planner)
builder.add_node(node=worker)
builder.add_node(node=reviewer)
builder.add_edge(source=planner, target=worker)
builder.add_edge(source=worker, target=reviewer)
builder.set_entry_point(node=planner)
