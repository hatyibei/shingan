"""Class-based DiGraphBuilder with self.<attr> agents (wild shape:
Austinggg/CreAgentive).

The canonical class-based idiom holds each agent on an instance attribute
(``self.user_proxy``) assigned from a factory / dict lookup, then builds the
graph from those attributes: ``builder.add_node(self.user_proxy)`` /
``builder.add_edge(self.user_proxy, self.extractor)``.

The agent references are ``ast.Attribute`` (``self.user_proxy``), not bare
``ast.Name``. The shim must resolve them to the trailing attribute name
(``user_proxy``) instead of returning None — otherwise add_node/add_edge
early-return and the ENTIRE class-based graph is dropped (false negative).

This file also has non-agent instance attributes (``self.model_client``,
``self.graph_flow``) that are NEVER passed to add_node/add_edge — they must NOT
become phantom nodes.
"""
from autogen_agentchat.teams import DiGraphBuilder, GraphFlow
from autogen_agentchat.conditions import MaxMessageTermination


class InitWorkflow:
    def __init__(self, model_client):
        self.model_client = model_client  # NOT a graph node.
        self.graph_flow = None            # NOT a graph node.
        self.user_proxy = None
        self.extractor = None
        self.validator = None
        self.structurer = None

    def _create_agents(self):
        agents = create_agents(self.model_client)
        self.user_proxy = agents["user_proxy"]
        self.extractor = agents["extractor"]
        self.validator = agents["validator"]
        self.structurer = agents["structurer"]

    def _build_graph(self):
        builder = DiGraphBuilder()

        builder.add_node(self.user_proxy)
        builder.add_node(self.extractor)
        builder.add_node(self.validator)
        builder.add_node(self.structurer)

        builder.add_edge(self.user_proxy, self.extractor)
        builder.add_edge(self.extractor, self.validator)
        # Conditional back-edge closes a cycle with a structural exit
        # (validator -> structurer) leaving it.
        builder.add_edge(self.validator, self.user_proxy, condition="incomplete")
        builder.add_edge(self.validator, self.structurer, condition="complete")

        builder.set_entry_point(self.user_proxy)
        self.graph = builder.build()

    def _create_graph_flow(self):
        self.graph_flow = GraphFlow(
            participants=[
                self.user_proxy,
                self.extractor,
                self.validator,
                self.structurer,
            ],
            graph=self.graph,
            termination_condition=MaxMessageTermination(20),
        )
