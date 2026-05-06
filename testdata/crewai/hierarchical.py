"""Hierarchical CrewAI workflow — manager LLM dispatches two workers.

The manager is an LLM that decides at runtime which worker to invoke. Per
ADR-013, our static analysis treats the manager as fanning out to every
worker (over-approximation), which keeps reachability analysis correct at
the cost of some `over_approximated_dynamic` confidence on those edges.

Expected findings: depending on rule activation, `circular_dep_agents`
might fire on the manager↔worker bidirectional pattern with low confidence.
"""
from crewai import Agent, Task, Crew, Process
from langchain_openai import ChatOpenAI

researcher = Agent(
    role="researcher",
    goal="Research factual data",
    backstory="A data scientist.",
    allow_delegation=False,
)

writer = Agent(
    role="writer",
    goal="Write the report",
    backstory="A technical writer.",
    allow_delegation=False,
)

t_research = Task(
    description="Research the latest market trends",
    expected_output="A research report",
    agent=researcher,
)

t_write = Task(
    description="Write a final summary",
    expected_output="An executive summary",
    agent=writer,
)

crew = Crew(
    agents=[researcher, writer],
    tasks=[t_research, t_write],
    process=Process.hierarchical,
    manager_llm=ChatOpenAI(model="gpt-4o-mini", temperature=0.0),
)
