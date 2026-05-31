"""Pure-loop AutoGen GraphFlow with NO structural exit edge.

generator <-> critic with no edge leaving the cycle. AutoGen would terminate
this externally (MaxMessageTermination passed to GraphFlow), but the graph
ITSELF has no structural exit, so shingan's cycle_detection keeps it Critical.

This is an accepted/correct PoC modelling boundary: a graph-level unbounded
loop is worth surfacing. The acceptance fixture (cycle_with_exit.py) is the one
that downgrades to Warning; this one documents the boundary where a pure loop
relying solely on external termination stays Critical.
"""
from autogen_agentchat.agents import AssistantAgent
from autogen_agentchat.teams import DiGraphBuilder, GraphFlow
from autogen_agentchat.conditions import MaxMessageTermination


generator = AssistantAgent(name="generator", model_client=None)
critic = AssistantAgent(name="critic", model_client=None)

builder = DiGraphBuilder()
builder.add_node(generator)
builder.add_node(critic)
builder.add_edge(generator, critic)
builder.add_edge(critic, generator)
builder.set_entry_point(generator)

flow = GraphFlow(
    participants=[generator, critic],
    graph=builder.build(),
    termination_condition=MaxMessageTermination(20),
)
