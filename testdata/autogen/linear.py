"""Linear AutoGen GraphFlow workflow: researcher -> writer -> editor.

Nodes are agents added via ``builder.add_node(agent)``; the node id is the
agent's ``name=`` string resolved from its ``AssistantAgent(name="...")``
binding. Edges come from ``builder.add_edge(A, B)``.

AutoGen has no in-graph exit sentinel — termination is external
(MaxMessageTermination passed to GraphFlow), so there is no END node and no
has_exit_branch synthesised here.
"""
from autogen_agentchat.agents import AssistantAgent
from autogen_agentchat.teams import DiGraphBuilder, GraphFlow
from autogen_agentchat.conditions import MaxMessageTermination


researcher = AssistantAgent(name="researcher", model_client=None)
writer = AssistantAgent(name="writer", model_client=None)
editor = AssistantAgent(name="editor", model_client=None)

builder = DiGraphBuilder()
builder.add_node(researcher)
builder.add_node(writer)
builder.add_node(editor)
builder.add_edge(researcher, writer)
builder.add_edge(writer, editor)

flow = GraphFlow(
    participants=[researcher, writer, editor],
    graph=builder.build(),
    termination_condition=MaxMessageTermination(10),
)
