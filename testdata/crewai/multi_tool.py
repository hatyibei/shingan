"""CrewAI Agent equipped with multiple tools, including code execution.

This fixture is designed to fire `eval_missing` (Critical) — the LLM agent
has unmediated access to a code-execution tool. Per ADR-013's NodeType
mapping, tools whose name matches `code_runner|python_repl|exec|eval` get
`Config["category"]="code_execution"`, which is the source for eval_missing.
"""
from crewai import Agent, Task, Crew, Process
from crewai_tools import BaseTool


class WebSearchTool(BaseTool):
    name: str = "web_search"
    description: str = "Search the web and return top results."

    def _run(self, query: str) -> str:
        return f"results for {query}"


class HttpRequestTool(BaseTool):
    name: str = "http_api_request"
    description: str = "Make an HTTP request to an arbitrary URL."

    def _run(self, url: str) -> str:
        return "..."


class PythonReplTool(BaseTool):
    name: str = "python_repl"
    description: str = "Execute arbitrary Python code and return the result."

    def _run(self, code: str) -> str:
        # In real CrewAI, this would invoke a sandbox. Real or not, Shingan
        # treats this as a code_execution sink for eval_missing detection.
        return str(eval(code))  # noqa: S307 — intentional vulnerable shape


agent = Agent(
    role="multi_tool_assistant",
    goal="Answer questions using whichever tool fits",
    backstory="A general-purpose assistant.",
    tools=[WebSearchTool(), HttpRequestTool(), PythonReplTool()],
    allow_delegation=False,
)

task = Task(
    description="Answer the user's question",
    expected_output="A short answer",
    agent=agent,
)

crew = Crew(
    agents=[agent],
    tasks=[task],
    process=Process.sequential,
)
