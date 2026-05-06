"""Two CrewAI Agents both with allow_delegation=True.

When both agents can delegate to each other, CrewAI's runtime can route
work back and forth indefinitely. Shingan's parser models this as
bidirectional `delegate` edges so `circular_dep_agents` rule has the
structural evidence it needs to fire (Warning).

Expected findings:
  - circular_dep_agents (Warning) on the alpha↔beta bidirectional pattern
  - cycle_detection may also fire (Critical/Warning, depending on tier)
"""
from crewai import Agent, Task, Crew, Process

alpha = Agent(
    role="alpha",
    goal="Coordinate with beta",
    backstory="Likes to delegate.",
    allow_delegation=True,
)

beta = Agent(
    role="beta",
    goal="Coordinate with alpha",
    backstory="Also likes to delegate.",
    allow_delegation=True,
)

t1 = Task(
    description="Plan the work",
    expected_output="A plan",
    agent=alpha,
)

t2 = Task(
    description="Execute the plan",
    expected_output="An outcome",
    agent=beta,
)

crew = Crew(
    agents=[alpha, beta],
    tasks=[t1, t2],
    process=Process.sequential,
)
