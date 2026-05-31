"""Cycle-with-conditional-exit acceptance fixture (ADR-015).

The graph forms a loop generator <-> critic:
    generator -> critic        (add_edge)
    critic    -> generator     (add_edge, condition="revise")  ← closes the cycle
    critic    -> finalizer      (add_edge, condition="approve") ← STRUCTURAL EXIT

AutoGen has NO in-graph exit sentinel — termination is external. So there is no
has_exit_branch here. Instead the cycle {generator, critic} has a structural
exit edge (critic -> finalizer) leaving the cycle to a real node. shingan's
cycle_detection (domain/rules/cycle.go cycleHasExit) therefore downgrades the
cycle from Critical to Warning WITHOUT any fake sentinel — proving the
framework-agnostic rules work on AutoGen graphs.

The entry is set explicitly with ``builder.set_entry_point(generator)``: the
generator has an incoming edge (critic -> generator) so it is not
zero-in-degree, and only an explicit entry pins it as the start.
"""
from autogen_agentchat.agents import AssistantAgent
from autogen_agentchat.teams import DiGraphBuilder, GraphFlow
from autogen_agentchat.conditions import MaxMessageTermination


generator = AssistantAgent(name="generator", model_client=None)
critic = AssistantAgent(name="critic", model_client=None)
finalizer = AssistantAgent(name="finalizer", model_client=None)

builder = DiGraphBuilder()
builder.add_node(generator)
builder.add_node(critic)
builder.add_node(finalizer)
builder.add_edge(generator, critic)
builder.add_edge(critic, generator, condition="revise")
builder.add_edge(critic, finalizer, condition="approve")
builder.set_entry_point(generator)

flow = GraphFlow(
    participants=[generator, critic, finalizer],
    graph=builder.build(),
    termination_condition=MaxMessageTermination(20),
)
