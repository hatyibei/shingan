"""Regression fixture for v0.8.7 FP-2: agents-only definition module.

Mirrors theyashwanthsai/Devyan agents.py — a class that declares
multiple `Agent(...)` ctors via factory methods, with no `Task(...)`
and no `Crew(...)` at module top-level. CrewAI's mental model is
task-centric (a Crew is its tasks; agents are resources bound by
`Task(agent=…)`), so an agents-only file isn't a workflow on its own
and should NOT produce `unreachable_node` findings on the agents.

Expected behaviour after the v0.8.7 fix: AST fallback returns an empty
graph (no nodes, no edges) so downstream rules find nothing to flag.
"""

from crewai import Agent


class CustomAgents:
    def architect_agent(self, tools):
        return Agent(
            role="Software Architect",
            goal="Design the system",
            backstory="Years of architectural experience",
            tools=tools,
            allow_delegation=False,
            verbose=True,
        )

    def programmer_agent(self, tools):
        return Agent(
            role="Software Programmer",
            goal="Implement the architecture",
            backstory="Detail-oriented engineer",
            tools=tools,
            allow_delegation=False,
            verbose=True,
        )

    def tester_agent(self, tools):
        return Agent(
            role="Software Tester",
            goal="Verify correctness",
            backstory="QA expert",
            tools=tools,
            allow_delegation=False,
            verbose=True,
        )

    def reviewer_agent(self, tools):
        return Agent(
            role="Software Reviewer",
            goal="Audit the deliverable",
            backstory="Senior reviewer",
            tools=tools,
            allow_delegation=False,
            verbose=True,
        )
