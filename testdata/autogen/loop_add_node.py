"""Loop-variable add_node idiom (wild shape: hugocool/FateForger).

The agents are bound via a factory helper (``_build_node`` here returns an
``AssistantAgent``), so the local variables are NOT statically bound to an
``*Agent(name=...)`` ctor — they fall back to the bare variable name. They are
then registered in a single ``for agent in (...): builder.add_node(agent)``
loop.

The loop control variable ``agent`` is an iteration placeholder, NOT a real
node: the shim must NOT register a phantom node literally named "agent"
(which previously surfaced as a false ``unreachable_node`` finding). The real
nodes (hydrate, assess, plan) come from the per-edge references.
"""
from autogen_agentchat.agents import AssistantAgent
from autogen_agentchat.teams import DiGraphBuilder, GraphFlow
from autogen_agentchat.conditions import MaxMessageTermination


def _build_node(name):
    # Factory: the binding is NOT a direct AssistantAgent(name=...) ctor, so the
    # local var resolves to its own identifier, exactly like the wild target.
    return AssistantAgent(name=name, model_client=None)


def build():
    builder = DiGraphBuilder()

    hydrate = _build_node("hydrate")
    assess = _build_node("assess")
    plan = _build_node("plan")

    # The phantom-prone idiom: register every agent through one loop variable.
    for agent in (hydrate, assess, plan):
        builder.add_node(agent)

    builder.add_edge(hydrate, assess)
    builder.add_edge(assess, plan)

    builder.set_entry_point(hydrate)

    return GraphFlow(
        participants=builder.get_participants(),
        graph=builder.build(),
        termination_condition=MaxMessageTermination(10),
    )
