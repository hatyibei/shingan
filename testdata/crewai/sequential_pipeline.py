"""Sequential 3-stage CrewAI pipeline — research → write → review.

Three Agents and three Tasks chained head-to-tail (Process.sequential).
The expected graph is a clean chain — no findings unless rules fire on
missing-eval / missing-error-handling defaults.
"""
from crewai import Agent, Task, Crew, Process

researcher = Agent(
    role="researcher",
    goal="Gather raw source material",
    backstory="A specialist librarian.",
    allow_delegation=False,
)

writer = Agent(
    role="writer",
    goal="Produce a draft from raw material",
    backstory="A long-form writer who values clarity.",
    allow_delegation=False,
)

reviewer = Agent(
    role="reviewer",
    goal="Catch factual and stylistic issues in the draft",
    backstory="An editor with 20 years of experience.",
    allow_delegation=False,
)

t_research = Task(
    description="Gather sources for the topic",
    expected_output="A list of citations",
    agent=researcher,
)

t_write = Task(
    description="Write a draft using the provided sources",
    expected_output="A markdown draft",
    agent=writer,
)

t_review = Task(
    description="Review the draft for accuracy and style",
    expected_output="A revised markdown draft",
    agent=reviewer,
)

crew = Crew(
    agents=[researcher, writer, reviewer],
    tasks=[t_research, t_write, t_review],
    process=Process.sequential,
)
