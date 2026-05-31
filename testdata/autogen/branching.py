"""Branching AutoGen GraphFlow workflow with conditional edges.

The router agent fans out to two specialists via conditional edges:
``builder.add_edge(router, billing, condition="billing")`` and
``builder.add_edge(router, tech, condition="technical")``. Both the A->B edge
AND the static condition keyword are extracted (the edge is emitted regardless
of whether the condition string resolves — dropping a conditional edge would
hide structural branches).
"""
from autogen_agentchat.agents import AssistantAgent
from autogen_agentchat.teams import DiGraphBuilder, GraphFlow
from autogen_agentchat.conditions import TextMentionTermination


router = AssistantAgent(name="router", model_client=None)
billing = AssistantAgent(name="billing", model_client=None)
tech = AssistantAgent(name="tech", model_client=None)

builder = DiGraphBuilder()
builder.add_node(router)
builder.add_node(billing)
builder.add_node(tech)
builder.add_edge(router, billing, condition="billing")
builder.add_edge(router, tech, condition="technical")

flow = GraphFlow(
    participants=[router, billing, tech],
    graph=builder.build(),
    termination_condition=TextMentionTermination("DONE"),
)
