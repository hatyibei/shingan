"""Minimal CrewAI workflow — single agent + single task.

This fixture exercises the parser's happy path: one Agent, one Task,
sequential Process. No tools, no delegation, no manager. The expected
WorkflowGraph has 2 nodes (1 LLM + 1 Tool) and 1 edge (Agent → Task).
"""
from crewai import Agent, Task, Crew, Process

researcher = Agent(
    role="researcher",
    goal="Find recent papers on a topic",
    backstory="A meticulous research assistant with a PhD in CS.",
    allow_delegation=False,
    verbose=False,
)

research_task = Task(
    description="Find 3 recent papers on workflow static analysis",
    expected_output="A markdown list of paper titles + abstracts",
    agent=researcher,
)

crew = Crew(
    agents=[researcher],
    tasks=[research_task],
    process=Process.sequential,
)
