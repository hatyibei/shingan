# NOT an AutoGen graph — NetworkX uses the same builder-pattern method names
# (add_node / add_edge). The parser must NOT mistake this for an AutoGen graph
# (codex review 2026-05-31, P1): only calls on a DiGraphBuilder receiver count.
import networkx as nx

a = "service-a"
b = "service-b"

g = nx.DiGraph()
g.add_node(a)
g.add_node(b)
g.add_edge(a, b)
g.add_edge(b, a)  # a cycle — must NOT be flagged (this isn't an AutoGen graph)
