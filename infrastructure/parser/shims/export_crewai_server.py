#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Long-lived JSON-RPC worker that exports CrewAI Crew/Agent/Task definitions
into Shingan's WorkflowGraph JSON format.

Protocol
========
Newline-delimited JSON (one request per line on stdin, one response per line
on stdout). All log output, crewai warnings, and tracebacks go to stderr —
stdout MUST contain only response frames.

Methods
-------
- ``health_check``   → ``{"status": "ok", "crewai_version": "x.y.z"}``
                      or ``{"status": "missing_crewai", ...}``
- ``parse_file``     → ``{"path": <abs path>}`` → WorkflowGraph JSON
- ``parse_content``  → ``{"content": <python source>, "filename": <hint>}``
                      → WorkflowGraph JSON
- ``shutdown``       → ``{"ok": true}`` then process exits.

Compatible with ``crewai >= 0.50`` (Pydantic v2 only); tolerant of API drift —
missing private attributes degrade to an empty graph rather than crashing the
worker.

ADR references
--------------
- ADR-013: CrewAI parser strategy reusing LangGraph PythonWorker.
- ADR-009: long-lived worker (no per-file fork) + degraded mode.
- ADR-008: ``over_approximated_dynamic`` ConfidenceReason for hierarchical
  manager-driven branching.
"""
from __future__ import annotations

import importlib
import importlib.util
import json
import os
import sys
import traceback
import types
from typing import Any, Dict, List, Optional, Tuple

# ----- stdout/stderr discipline ---------------------------------------------
# `crewai` prints telemetry and pydantic warnings on import. Re-route all
# "natural" stdout to stderr so that response framing on stdout stays clean.
_RESPONSE_STREAM = sys.stdout
sys.stdout = sys.stderr  # any stray print() now lands on stderr.

# Disable CrewAI telemetry so importing the user module doesn't emit network
# traffic or noisy warnings on stderr.
os.environ.setdefault("OTEL_SDK_DISABLED", "true")
os.environ.setdefault("CREWAI_TELEMETRY_OPT_OUT", "true")

# Provide dummy LLM provider credentials so user modules whose top-level
# code constructs OpenAI / Anthropic / Google clients at import time
# (the dominant CrewAI Flow pattern) don't raise `OpenAIError: Missing
# credentials` before the analysis can run. Validation happens at
# request time, and we monkey-patch all kickoff/execute paths to no-op
# stubs anyway, so no real API call is ever issued.
for _key in (
    "OPENAI_API_KEY",
    "ANTHROPIC_API_KEY",
    "GOOGLE_API_KEY",
    "GEMINI_API_KEY",
    "AZURE_OPENAI_API_KEY",
    "GROQ_API_KEY",
    "MISTRAL_API_KEY",
    "COHERE_API_KEY",
    "TOGETHER_API_KEY",
    "REPLICATE_API_TOKEN",
):
    os.environ.setdefault(_key, "shingan-static-analysis-stub")

# Redirect CrewAI's SQLite task-output storage away from $HOME so the worker
# still functions in read-only-HOME CI sandboxes (Codex iter2 P2). CrewAI
# follows XDG_DATA_HOME; pointing it at a writable temp dir avoids
# DatabaseOperationError ("unable to open database file") even when the user
# never actually invokes a Crew at runtime — Pydantic validators on Crew()
# touch storage during construction.
import tempfile
_default_xdg = os.path.join(tempfile.gettempdir(), "shingan-crewai-xdg")
try:
    os.makedirs(_default_xdg, exist_ok=True)
except OSError:  # noqa: BLE001 — best effort
    _default_xdg = tempfile.gettempdir()
os.environ.setdefault("XDG_DATA_HOME", _default_xdg)
os.environ.setdefault("CREWAI_STORAGE_DIR", _default_xdg)


def _emit(payload: Dict[str, Any]) -> None:
    """Write a JSON-RPC frame to the original stdout."""
    line = json.dumps(payload, ensure_ascii=False)
    _RESPONSE_STREAM.write(line)
    _RESPONSE_STREAM.write("\n")
    _RESPONSE_STREAM.flush()


def _err(req_id: Any, code: int, message: str, data: Optional[Any] = None) -> Dict[str, Any]:
    body: Dict[str, Any] = {"code": code, "message": message}
    if data is not None:
        body["data"] = data
    return {"id": req_id, "error": body}


# ----- crewai loading --------------------------------------------------------
def _load_crewai() -> Tuple[Optional[types.ModuleType], Optional[str]]:
    try:
        mod = importlib.import_module("crewai")
    except Exception as exc:  # noqa: BLE001 — any import failure
        return None, f"{type(exc).__name__}: {exc}"
    _stub_runtime_methods(mod)
    return mod, None


_RUNTIME_STUBS_INSTALLED = False


def _stub_runtime_methods(crewai_mod: types.ModuleType) -> None:
    """Monkey-patch Crew.kickoff / Task.execute / Agent.execute_task with
    no-op stubs so user modules whose top-level code does
    `crew.kickoff()` or `task.execute()` (a common production pattern)
    don't crash analysis. The stubs return an empty string and have no
    side effects (no API calls, no network, no disk writes).

    Idempotent — only patches once per worker lifetime. The original
    methods aren't restored: this worker only ever loads the module
    once, then exits, so leaking the stubs is fine.
    """
    global _RUNTIME_STUBS_INSTALLED
    if _RUNTIME_STUBS_INSTALLED:
        return
    _RUNTIME_STUBS_INSTALLED = True

    def _noop(self, *args, **kwargs):
        return ""

    # Stub these unconditionally — even if a method doesn't exist in the
    # currently-installed CrewAI version, user code written against an
    # older / newer API may call it (`task.execute()` is the classic case
    # — removed in CrewAI 1.x, but still present in legacy examples like
    # crewAI-examples/crews/screenplay_writer/screenplay_writer.py).
    for attr_path in (
        ("Crew", "kickoff"),
        ("Crew", "kickoff_for_each"),
        ("Crew", "kickoff_async"),
        ("Crew", "train"),
        ("Crew", "test"),
        ("Crew", "replay"),
        ("Task", "execute"),
        ("Task", "execute_sync"),
        ("Task", "execute_async"),
        ("Agent", "execute_task"),
        ("Agent", "kickoff"),
    ):
        cls_name, method = attr_path
        cls = getattr(crewai_mod, cls_name, None)
        if cls is not None:
            try:
                setattr(cls, method, _noop)
            except (AttributeError, TypeError):
                pass


def _crewai_version() -> str:
    try:
        mod = importlib.import_module("crewai")
        return getattr(mod, "__version__", "unknown")
    except Exception:  # noqa: BLE001
        return "missing"


# ----- module loader ---------------------------------------------------------
def _package_root(path: str) -> str:
    """Return the first ancestor of `path` that is *not* a Python package.

    A directory is considered a package when it contains `__init__.py`.
    Walking up and stopping at the first non-package gives us the
    directory Python's import system needs on `sys.path` for both
    relative and absolute imports inside the user's package to resolve.
    """
    abs_path = os.path.abspath(path)
    dir_ = os.path.dirname(abs_path)
    seen = set()
    while dir_ and dir_ not in seen:
        seen.add(dir_)
        init = os.path.join(dir_, "__init__.py")
        if not os.path.exists(init):
            return dir_
        parent = os.path.dirname(dir_)
        if parent == dir_:
            return dir_
        dir_ = parent
    return os.path.dirname(abs_path)


def _dotted_name_for(path: str, pkg_root: str) -> str:
    abs_path = os.path.abspath(path)
    rel = os.path.relpath(abs_path, pkg_root)
    parts = rel.replace(os.sep, "/").split("/")
    if parts[-1].endswith(".py"):
        parts[-1] = parts[-1][:-3]
    if parts[-1] == "__init__":
        parts = parts[:-1]
    parts = [p for p in parts if p]
    if not parts:
        return "_shingan_user_" + abs_path.replace(os.sep, "_")
    return ".".join(parts)


def _is_self_or_parent(missing: str, dotted: str) -> bool:
    if missing == dotted:
        return True
    return dotted.startswith(missing + ".")


def _register_parent_packages(dotted: str, pkg_root: str) -> list[str]:
    parts = dotted.split(".")
    inserted: list[str] = []
    for i in range(1, len(parts)):
        parent_name = ".".join(parts[:i])
        if parent_name in sys.modules:
            continue
        parent_dir = os.path.join(pkg_root, *parts[:i])
        stub = types.ModuleType(parent_name)
        stub.__path__ = [parent_dir]
        stub.__file__ = os.path.join(parent_dir, "__init__.py")
        sys.modules[parent_name] = stub
        inserted.append(parent_name)
    return inserted


def _import_user_module(path: str) -> types.ModuleType:
    """Try `importlib.import_module(dotted)` first so parent
    `__init__.py` side effects fire (relative imports, attribute
    population). Fall back to `spec_from_file_location` for files
    outside any package. See the LangGraph shim for full rationale.
    """
    abs_path = os.path.abspath(path)
    src_dir = os.path.dirname(abs_path)
    pkg_root = _package_root(abs_path)
    dotted = _dotted_name_for(abs_path, pkg_root)

    inserted_paths: list[str] = []
    for d in (pkg_root, src_dir):
        if d and d not in sys.path:
            sys.path.insert(0, d)
            inserted_paths.append(d)
    prev_cwd = os.getcwd()
    inserted_modules: list[str] = []
    inserted_self = False
    try:
        if src_dir and os.path.isdir(src_dir):
            os.chdir(src_dir)
        if "." in dotted and not dotted.startswith("_shingan_user_"):
            try:
                return importlib.import_module(dotted)
            except ModuleNotFoundError as exc:
                missing = getattr(exc, "name", "") or ""
                if missing and not _is_self_or_parent(missing, dotted):
                    raise  # third-party dep gap — surface via UX wrapper
                # Otherwise fall through to spec_from_file_location.
            except ImportError:
                pass
        inserted_modules = _register_parent_packages(dotted, pkg_root)
        spec = importlib.util.spec_from_file_location(dotted, abs_path)
        if spec is None or spec.loader is None:
            raise ImportError(f"could not load {abs_path}")
        module = importlib.util.module_from_spec(spec)
        sys.modules[dotted] = module
        inserted_self = True
        spec.loader.exec_module(module)  # type: ignore[union-attr]
        return module
    finally:
        try:
            os.chdir(prev_cwd)
        except OSError:
            pass
        if inserted_self:
            sys.modules.pop(dotted, None)
        for name in inserted_modules:
            sys.modules.pop(name, None)
        for d in inserted_paths:
            try:
                sys.path.remove(d)
            except ValueError:
                pass


def _import_user_source(content: str, filename: str) -> types.ModuleType:
    src_dir = ""
    if filename and not filename.startswith("<"):
        try:
            abs_path = os.path.abspath(filename)
            src_dir = os.path.dirname(abs_path)
        except (TypeError, ValueError):
            src_dir = ""
    inserted = False
    if src_dir and src_dir not in sys.path:
        sys.path.insert(0, src_dir)
        inserted = True
    try:
        module = types.ModuleType(f"_shingan_inline_{abs(hash(filename))}")
        module.__file__ = filename or "<inline>"
        code = compile(content, filename or "<inline>", "exec")
        exec(code, module.__dict__)  # noqa: S102 — intentional
        return module
    finally:
        if inserted:
            try:
                sys.path.remove(src_dir)
            except ValueError:
                pass


# ----- Crew / Agent / Task detection -----------------------------------------
def _is_crew(obj: Any) -> bool:
    cls = type(obj)
    if cls.__name__ == "Crew":
        return True
    for base in cls.__mro__:
        if base.__name__ == "Crew":
            return True
    return False


def _is_agent(obj: Any) -> bool:
    cls = type(obj)
    if cls.__name__ == "Agent":
        return True
    for base in cls.__mro__:
        if base.__name__ == "Agent":
            return True
    return False


def _is_task(obj: Any) -> bool:
    cls = type(obj)
    if cls.__name__ == "Task":
        return True
    for base in cls.__mro__:
        if base.__name__ == "Task":
            return True
    return False


def _read(obj: Any, *names: str, default: Any = None) -> Any:
    for n in names:
        if hasattr(obj, n):
            v = getattr(obj, n)
            if v is not None:
                return v
    return default


def _stringify(obj: Any, max_len: int = 500) -> str:
    """Coerce an arbitrary value into a string suitable for Config storage.

    Long strings are truncated so the resulting JSON stays compact.
    """
    try:
        s = str(obj) if obj is not None else ""
    except Exception:  # noqa: BLE001
        s = "<unprintable>"
    if len(s) > max_len:
        return s[:max_len] + "…"
    return s


# ----- Tool flattening -------------------------------------------------------
def _tool_id(crew_id: str, tool_name: str) -> str:
    return f"{crew_id}::tool::{_safe_id(tool_name)}"


def _agent_id(crew_id: str, agent_name: str) -> str:
    return f"{crew_id}::agent::{_safe_id(agent_name)}"


def _task_id(crew_id: str, task_name: str) -> str:
    return f"{crew_id}::task::{_safe_id(task_name)}"


def _safe_id(name: str) -> str:
    out = []
    for ch in str(name).strip():
        if ch.isalnum() or ch in "_-./":
            out.append(ch)
        elif ch.isspace():
            out.append("_")
    return "".join(out).strip("_") or "node"


def _pydantic_to_schema(args: Any) -> Optional[Dict[str, Any]]:
    """Serialise a Pydantic model class (or instance) to a JSON schema dict.

    Tries Pydantic v2's `model_json_schema()` first, then falls back to v1's
    `schema()`. Returns the dict on success, None when neither is available
    (e.g. a plain Python class or a dict was passed). Used so the
    `unbounded_tool_arg` rule — which expects a map-valued JSON schema —
    can fire on CrewAI tools whose `args_schema` is a Pydantic model
    (Codex iter6 P2).
    """
    if isinstance(args, dict):
        return args
    # Both class-level and instance-level access work.
    for method in ("model_json_schema", "schema"):
        fn = getattr(args, method, None)
        if callable(fn):
            try:
                result = fn()
            except TypeError:
                # `schema()` is sometimes an unbound method on the class;
                # call with the type itself as `self`.
                try:
                    result = fn(args)  # type: ignore[arg-type]
                except Exception:  # noqa: BLE001
                    continue
            except Exception:  # noqa: BLE001
                continue
            if isinstance(result, dict):
                return result
    return None


def _detect_tool_category(tool: Any, name: str) -> str:
    """Map a CrewAI tool to a Shingan Config["category"].

    Heuristics are deliberately conservative — a tool we don't recognise gets
    `category="tool"` so the rest of the pipeline still works.
    """
    lname = name.lower()
    cls_name = type(tool).__name__.lower() if tool is not None else ""
    if any(tok in lname for tok in ("eval", "exec", "code_runner", "code_interpreter",
                                    "python_repl", "shell", "bash", "subprocess")):
        return "code_execution"
    if any(tok in cls_name for tok in ("codeinterp", "pythonrepl", "shelltool")):
        return "code_execution"
    if any(tok in lname for tok in ("http", "api", "request", "fetch", "rest")):
        return "api"
    if any(tok in lname for tok in ("search", "browser", "scrape", "web")):
        return "tool"
    return "tool"


def _agent_model(agent: Any) -> Tuple[str, Dict[str, Any]]:
    """Pull the (model, extra-config) pair for an Agent.

    CrewAI exposes the LLM either as `Agent.llm` (string or LLM object) or via
    `Agent.config['llm']`. We surface model name + provider + temperature so
    rules like `temperature_misuse` and `model_card_mismatch` fire.
    """
    cfg: Dict[str, Any] = {}
    llm = _read(agent, "llm")
    if isinstance(llm, str):
        cfg["model"] = llm
    elif llm is not None:
        cfg["model"] = _stringify(_read(llm, "model_name", "model", default=type(llm).__name__))
        prov = _read(llm, "provider", "_llm_type", "model_provider")
        if prov:
            cfg["provider"] = _stringify(prov)
        temp = _read(llm, "temperature")
        if temp is not None:
            try:
                cfg["temperature"] = float(temp)
            except (TypeError, ValueError):
                pass
        base_url = _read(llm, "base_url", "api_base")
        if base_url:
            cfg["base_url"] = _stringify(base_url)
    model = cfg.get("model", "unknown")
    return str(model), cfg


# ----- WorkflowGraph builder -------------------------------------------------
def _build_graph(crew: Any, source_path: str) -> Dict[str, Any]:
    """Convert a CrewAI Crew object into a Shingan WorkflowGraph JSON.

    Structural mapping (per ADR-013):
      * Crew              → graph metadata only (the Crew itself isn't a node)
      * Agent             → NodeTypeLLM (one per agent)
      * Task              → NodeTypeTool (one per task)
      * Each agent.tools  → NodeTypeTool (one per tool, deduplicated)
      * Process.sequential → Tasks linked head-to-tail (Task[i] → Task[i+1])
      * Process.hierarchical → manager → every worker (over-approximation;
                               no `worker → manager` back-edge so we don't
                               manufacture false cycle_detection findings —
                               the result-return path isn't a real graph
                               edge). Manager → Task one-shot dispatches.
      * Agent.tools[t]    → Edge(agent_id → tool_id), unconditional
      * Task → Agent      → Edge(task_id → agent_id), unconditional
    Edge.Condition is reserved for actual conditional dispatch (e.g.
    `delegate` for manager_llm choosing worker, or bidirectional delegate
    cycles); free-text labels are never written into Condition because
    error_handler_checker treats any non-empty value as a try/catch-style
    fallback (Codex iter5 P2).
    """
    out_nodes: List[Dict[str, Any]] = []
    out_edges: List[Dict[str, Any]] = []
    seen: Dict[str, bool] = {}

    crew_name = _stringify(_read(crew, "name", default="crew"), max_len=80) or "crew"
    crew_id = _safe_id(crew_name)

    # ---- 1. Agents -----------------------------------------------------------
    agents = _read(crew, "agents", default=[]) or []
    if not isinstance(agents, (list, tuple)):
        agents = list(agents) if hasattr(agents, "__iter__") else []

    agent_id_by_obj: Dict[int, str] = {}
    agent_id_by_role: Dict[str, str] = {}
    agent_tools: List[Tuple[str, Any, str]] = []  # (agent_id, tool, tool_name)
    # Counter for disambiguating duplicate-role agents (Codex iter7 P2):
    # if two distinct Agent objects share role="researcher", we'd
    # otherwise collapse them into one node and lose one agent's tools
    # / config. Suffix subsequent collisions with `-2`, `-3`, ….
    role_collision_count: Dict[str, int] = {}

    for agent in agents:
        if not _is_agent(agent):
            continue
        role = _stringify(_read(agent, "role", default="agent"), max_len=120) or "agent"
        a_id = _agent_id(crew_id, role)
        if a_id in agent_id_by_obj.values():
            # Duplicate role: append index so we get distinct node IDs.
            role_collision_count[role] = role_collision_count.get(role, 1) + 1
            a_id = _agent_id(crew_id, f"{role}-{role_collision_count[role]}")
        agent_id_by_obj[id(agent)] = a_id
        agent_id_by_role.setdefault(role, a_id)

        model, llm_cfg = _agent_model(agent)
        cfg: Dict[str, Any] = {"agent_role": role}
        cfg.update(llm_cfg)
        goal = _read(agent, "goal")
        if goal:
            cfg["goal"] = _stringify(goal)
        backstory = _read(agent, "backstory")
        if backstory:
            cfg["backstory"] = _stringify(backstory)
        delegation = _read(agent, "allow_delegation", default=False)
        cfg["allow_delegation"] = bool(delegation) if delegation is not None else False
        if model:
            cfg["model"] = model

        # Track sub-agents (delegation targets) for circular_dep_agents rule.
        sub_agents_raw = _read(agent, "sub_agents", default=None)
        if isinstance(sub_agents_raw, (list, tuple)) and sub_agents_raw:
            cfg["sub_agents"] = [_stringify(_read(s, "role", default=type(s).__name__))
                                 for s in sub_agents_raw if s is not None]

        if a_id not in seen:
            seen[a_id] = True
            out_nodes.append({
                "id":     a_id,
                "name":   role,
                "type":   "llm",
                "config": cfg,
                "pos":    {"file": source_path, "line": 0, "col": 0},
            })

        # Collect tools attached to this agent.
        tools = _read(agent, "tools", default=[]) or []
        if isinstance(tools, (list, tuple)):
            for t in tools:
                tname = _stringify(_read(t, "name", "_name", default=type(t).__name__),
                                   max_len=80) or type(t).__name__
                agent_tools.append((a_id, t, tname))

    # ---- 2. Agent.tools → Tool nodes + Edges --------------------------------
    # `tool_seen` is shared with section 4 so manager_agent's tools (which
    # bypass the regular `agents` enumeration) can be emitted with the same
    # deduplication semantics (Codex iter3 P2).
    tool_seen: Dict[str, bool] = {}

    def _emit_agent_tools(a_id: str, tools_iter: List[Tuple[str, Any, str]]) -> None:
        for src_id, tool, tname in tools_iter:
            t_id = _tool_id(crew_id, tname)
            if t_id not in tool_seen:
                tool_seen[t_id] = True
                cat = _detect_tool_category(tool, tname)
                tool_cfg: Dict[str, Any] = {"category": cat, "tool_name": tname}
                desc = _read(tool, "description")
                if desc:
                    tool_cfg["description"] = _stringify(desc)
                args = _read(tool, "args_schema", "input_schema")
                if args is not None:
                    # Pydantic schemas → serialise to JSON schema dict so
                    # `unbounded_tool_arg` (which scans map-valued JSON
                    # schemas for missing maxLength/maxItems/maximum
                    # constraints) can fire on CrewAI tools too. Try
                    # Pydantic v2 model_json_schema() first, then v1
                    # schema(); fall back to the class name for plain
                    # types we can't introspect (Codex iter6 P2).
                    schema = _pydantic_to_schema(args)
                    if schema is not None:
                        tool_cfg["args_schema"] = schema
                    else:
                        tool_cfg["args_schema"] = type(args).__name__
                out_nodes.append({
                    "id":     t_id,
                    "name":   tname,
                    "type":   "tool",
                    "config": tool_cfg,
                    "pos":    {"file": source_path, "line": 0, "col": 0},
                })
            # Codex iter5 P2: leave Condition empty for non-control-flow
            # labels — error_handler_checker treats any non-empty
            # Condition as conditional fallback, which would incorrectly
            # mute findings on Agents that have tools but no actual
            # error-handling branch. The "uses_tool" relationship lives
            # in the topology itself, not in the Condition string.
            out_edges.append({"from": src_id, "to": t_id})

    _emit_agent_tools("", agent_tools)

    # ---- 3. Tasks → Tool nodes + sequential / hierarchical edges -----------
    tasks = _read(crew, "tasks", default=[]) or []
    if not isinstance(tasks, (list, tuple)):
        tasks = list(tasks) if hasattr(tasks, "__iter__") else []

    task_ids: List[str] = []
    for i, task in enumerate(tasks):
        if not _is_task(task):
            continue
        desc = _stringify(_read(task, "description", default=f"task_{i}"), max_len=120)
        # Many CrewAI tasks share the same description prefix; disambiguate
        # by index so edges land on the right destination. Use `-` rather
        # than `#` because `_safe_id` strips `#` (Codex iter4 P2): task
        # descriptions like "a1" + "a" at indices 0 / 10 would otherwise
        # both collapse to "a10".
        t_label = f"{desc[:60]}-{i}" if desc else f"task_{i}"
        t_id = _task_id(crew_id, t_label)
        task_ids.append(t_id)

        cfg: Dict[str, Any] = {"description": desc}
        expected = _read(task, "expected_output")
        if expected:
            cfg["expected_output"] = _stringify(expected)
        agent_obj = _read(task, "agent")
        if agent_obj is not None:
            assigned = agent_id_by_obj.get(id(agent_obj))
            if not assigned:
                role = _stringify(_read(agent_obj, "role", default=""), max_len=120)
                assigned = agent_id_by_role.get(role)
            if assigned:
                cfg["assigned_agent"] = assigned
                # Edge Task → Agent so the Agent (and its Tools) is reachable
                # from the entry Task. Mental model: the Task pulls in the
                # Agent's expertise during execution.
                # Codex iter5 P2: no Condition — see _emit_agent_tools.
                out_edges.append({"from": t_id, "to": assigned})
        async_exec = _read(task, "async_execution", default=False)
        if async_exec:
            cfg["async_execution"] = True

        if t_id not in seen:
            seen[t_id] = True
            out_nodes.append({
                "id":     t_id,
                "name":   t_label[:80],
                "type":   "tool",
                "config": cfg,
                "pos":    {"file": source_path, "line": 0, "col": 0},
            })

    # ---- 4. Process: sequential vs hierarchical ----------------------------
    process_obj = _read(crew, "process")
    process_str = _stringify(process_obj).lower() if process_obj is not None else "sequential"

    if "hierarchical" in process_str:
        # manager → every worker, every worker → manager.
        # CrewAI 1.x supports two configurations:
        #   - manager_agent=Agent(...)  (custom Agent, often NOT in crew.agents)
        #   - manager_llm=LLM(...)      (synthetic manager built from an LLM)
        # Codex iter2 P2: handle manager_agent first since custom managers
        # carry richer metadata (role/goal/tools) and are usually outside
        # the regular agent list — falling through to agents[0] would
        # produce wrong delegate/report edges.
        manager_id = ""
        manager_agent = _read(crew, "manager_agent")
        manager_llm = _read(crew, "manager_llm")

        if manager_agent is not None and _is_agent(manager_agent):
            # Reuse the existing Agent → node pipeline. Compute the same id
            # the agents loop would, then materialise the node if it isn't
            # already in seen[] (manager_agent is typically NOT in agents).
            mrole = _stringify(_read(manager_agent, "role", default="manager"),
                               max_len=120) or "manager"
            # Codex iter8 P2: if a worker in crew.agents already claimed
            # this role's ID, suffix to avoid collapsing distinct objects
            # (e.g. manager_agent and a worker both labelled "manager").
            # Without this the manager_agent's tools/config are silently
            # dropped and delegate edges originate from the worker.
            existing = agent_id_by_obj.get(id(manager_agent))
            if existing:
                manager_id = existing
            else:
                manager_id = _agent_id(crew_id, mrole)
                if manager_id in agent_id_by_obj.values():
                    role_collision_count[mrole] = role_collision_count.get(mrole, 1) + 1
                    manager_id = _agent_id(crew_id, f"{mrole}-{role_collision_count[mrole]}")
                agent_id_by_obj[id(manager_agent)] = manager_id
                agent_id_by_role.setdefault(mrole, manager_id)
            if manager_id not in seen:
                seen[manager_id] = True
                mmodel, mllm_cfg = _agent_model(manager_agent)
                mcfg: Dict[str, Any] = {
                    "agent_role": mrole,
                    "is_manager": True,
                }
                mcfg.update(mllm_cfg)
                if mmodel:
                    mcfg["model"] = mmodel
                mgoal = _read(manager_agent, "goal")
                if mgoal:
                    mcfg["goal"] = _stringify(mgoal)
                out_nodes.append({
                    "id":     manager_id,
                    "name":   mrole,
                    "type":   "llm",
                    "config": mcfg,
                    "pos":    {"file": source_path, "line": 0, "col": 0},
                })
            # Codex iter3 P2: also emit any tools attached to the manager
            # agent so eval_missing / unbounded_tool_arg etc. can fire on
            # `manager → tool` paths (e.g. a manager equipped with
            # `python_repl`).
            mgr_tools = _read(manager_agent, "tools", default=[]) or []
            if isinstance(mgr_tools, (list, tuple)) and mgr_tools:
                pairs: List[Tuple[str, Any, str]] = []
                for t in mgr_tools:
                    tname = _stringify(_read(t, "name", "_name",
                                             default=type(t).__name__),
                                        max_len=80) or type(t).__name__
                    pairs.append((manager_id, t, tname))
                _emit_agent_tools(manager_id, pairs)
        elif manager_llm is not None:
            mname = _stringify(_read(manager_llm, "model_name", "model",
                                     default=type(manager_llm).__name__),
                                max_len=80) or "manager_llm"
            manager_id = _agent_id(crew_id, "manager_" + mname)
            if manager_id not in seen:
                seen[manager_id] = True
                mcfg = {
                    "agent_role": "manager",
                    "model":      mname,
                    "is_manager": True,
                }
                mtemp = _read(manager_llm, "temperature")
                if mtemp is not None:
                    try:
                        mcfg["temperature"] = float(mtemp)
                    except (TypeError, ValueError):
                        pass
                out_nodes.append({
                    "id":     manager_id,
                    "name":   "manager",
                    "type":   "llm",
                    "config": mcfg,
                    "pos":    {"file": source_path, "line": 0, "col": 0},
                })
        elif agents:
            # Fall back to first declared agent acting as manager.
            first_id = agent_id_by_obj.get(id(agents[0]))
            if first_id:
                manager_id = first_id

        if manager_id:
            for agent in agents:
                worker_id = agent_id_by_obj.get(id(agent))
                if not worker_id or worker_id == manager_id:
                    continue
                # Manager → worker is a real conditional dispatch
                # (manager_llm chooses which worker), so keep the
                # Condition. Codex iter5 P2: drop the worker → manager
                # `report` back-edge — it modelled the result return
                # path, not a graph dispatch, and was forming false
                # 2-node cycles that fired cycle_detection Critical on
                # otherwise valid hierarchical workflows.
                out_edges.append({"from": manager_id, "to": worker_id, "condition": "delegate"})
            # Manager → Task: connect the manager to every Task it
            # orchestrates. No Condition (the relationship is the edge
            # itself; "manage" was leaking through error_handler_checker
            # as a false fallback).
            for tid in task_ids:
                out_edges.append({"from": manager_id, "to": tid})
        entry_node_id = manager_id or (task_ids[0] if task_ids else "")
    else:
        # Sequential: link Tasks head-to-tail.
        for i in range(len(task_ids) - 1):
            out_edges.append({"from": task_ids[i], "to": task_ids[i + 1]})
        entry_node_id = task_ids[0] if task_ids else (
            next(iter(agent_id_by_obj.values()), "") if agent_id_by_obj else ""
        )

    # ---- 5. Delegation cycles (allow_delegation=True) ----------------------
    # If multiple agents have delegation enabled, the runtime can route work
    # between them. Mirror this as bidirectional edges so circular_dep_agents
    # rule has structural evidence to fire (Confidence handled by the rule).
    delegating: List[str] = []
    for agent in agents:
        if _read(agent, "allow_delegation", default=False):
            aid = agent_id_by_obj.get(id(agent))
            if aid:
                delegating.append(aid)
    if len(delegating) >= 2:
        for i, src in enumerate(delegating):
            for dst in delegating[i + 1:]:
                out_edges.append({"from": src, "to": dst, "condition": "delegate"})
                out_edges.append({"from": dst, "to": src, "condition": "delegate"})

    return {
        "nodes": out_nodes,
        "edges": out_edges,
        "entry_node_id": entry_node_id,
        "metadata": {
            "source_format":           "crewai",
            "source_file":             source_path,
            "crewai_version":          _crewai_version(),
            "process":                 process_str,
            "conditional_edge_reason": "over_approximated_dynamic" if "hierarchical" in process_str else "exact_static_match",
        },
    }


def _find_crew(module: types.ModuleType) -> Any:
    """Walk module globals to find a Crew instance.

    Resolution order:
      1. Module-level Crew instances (`crew = Crew(...)` at top level).
      2. Module-level objects with `.crew` attribute (CrewBase patterns).
      3. **Zero-arg factory functions returning Crew** — real-world code
         (crewAI-examples) constructs the Crew inside `def crew()` /
         `def make_crew()` rather than at module top level. Try calling
         such functions; catch all exceptions so a broken factory
         (missing API key, network call) doesn't kill the worker.
    """
    # Pass 1: top-level instances + attribute hops.
    for _name, value in vars(module).items():
        if value is module:
            continue
        if _is_crew(value):
            return value
        attr = getattr(value, "crew", None)
        if attr is not None and _is_crew(attr):
            return attr

    # Pass 2: zero-arg factory functions (e.g. `def crew(): return Crew(...)`).
    for name, value in vars(module).items():
        if not callable(value) or isinstance(value, type):
            continue
        if value is module:
            continue
        try:
            value_module = getattr(value, "__module__", None)
            if value_module and value_module != module.__name__:
                continue
        except Exception:  # noqa: BLE001
            continue
        if not _has_only_optional_args(value):
            continue
        try:
            result = value()
        except Exception:  # noqa: BLE001 — factory may need API keys / deps
            continue
        if _is_crew(result):
            return result
        attr = getattr(result, "crew", None)
        if attr is not None and _is_crew(attr):
            return attr

    # Pass 3: @CrewBase-decorated classes (the dominant modern pattern in
    # crewAI-examples). The class needs to be instantiated, then its
    # `crew()` method called, to produce the actual Crew. We guard with
    # try/except per step so missing config files / API keys don't kill
    # the worker.
    for name, value in vars(module).items():
        if not isinstance(value, type):
            continue
        try:
            value_module = getattr(value, "__module__", None)
            if value_module and value_module != module.__name__:
                continue
        except Exception:  # noqa: BLE001
            continue
        if not _has_only_optional_args(value.__init__):
            # Class needs constructor args we can't synthesise.
            continue
        try:
            instance = value()
        except Exception:  # noqa: BLE001
            continue
        # Try instance.crew() first (CrewBase pattern).
        crew_method = getattr(instance, "crew", None)
        if callable(crew_method):
            try:
                produced = crew_method()
            except Exception:  # noqa: BLE001
                produced = None
            if _is_crew(produced):
                return produced
        # Otherwise, instance.crew may already be the Crew instance.
        if _is_crew(crew_method):
            return crew_method

    return None


def _has_only_optional_args(fn: Any) -> bool:
    """Return True when fn has no required positional/keyword arguments.

    Used by `_find_crew` to skip factories that require config (LLM
    model, API key, etc.) — calling those without args would just raise
    TypeError without producing useful structure for the analysis.

    Note: `self` (in unbound `__init__` methods) and `cls` (classmethods)
    aren't real "required arguments" — Python supplies them via the
    instance/class binding. We skip the first positional parameter when
    its name is one of these standard placeholders so @CrewBase classes
    with `def __init__(self, *args, **kwargs)` are correctly recognised
    as zero-argument-instantiable.
    """
    try:
        import inspect as _inspect
        sig = _inspect.signature(fn)
    except (TypeError, ValueError):
        return False
    params = list(sig.parameters.values())
    if params and params[0].name in ("self", "cls"):
        params = params[1:]
    for p in params:
        if p.kind in (_inspect.Parameter.VAR_POSITIONAL,
                      _inspect.Parameter.VAR_KEYWORD):
            continue
        if p.default is _inspect.Parameter.empty:
            return False
    return True


# ----- handlers --------------------------------------------------------------
def _handle_health_check(_: Dict[str, Any]) -> Dict[str, Any]:
    mod, err = _load_crewai()
    if mod is None:
        return {"status": "missing_crewai", "error": err, "python": sys.version}
    return {
        "status":          "ok",
        "crewai_version":  getattr(mod, "__version__", "unknown"),
        "python":          sys.version,
    }


def _handle_parse_file(params: Dict[str, Any]) -> Dict[str, Any]:
    path = params.get("path")
    if not path:
        raise ValueError("parse_file: 'path' is required")
    mod, err = _load_crewai()
    if mod is None:
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(f"crewai is not importable: {err}")
    try:
        user_module = _import_user_module(path)
    except ModuleNotFoundError as exc:
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(_missing_dep_message(path, exc))
    except Exception:  # noqa: BLE001 — symmetry with LangGraph shim
        # Modules with side-effects at import (LLM client init,
        # network-touching loaders, deprecated module shims like
        # `langchain.llms` re-export wrappers) raise non-ModuleNotFound
        # exceptions before Crew/Agent/Task are in scope. AST fallback
        # walks the syntax tree without executing the module, so route
        # through it before giving up. Dogfood: crewAI-examples
        # crews/instagram_post/main.py raises RuntimeError on
        # `from langchain.llms import ...` re-export.
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise
    crew = _find_crew(user_module)
    if crew is None:
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        return _build_graph(types.SimpleNamespace(agents=[], tasks=[]), path)
    runtime_graph = _build_graph(crew, path)
    if not runtime_graph["nodes"]:
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
    return runtime_graph


def _handle_parse_content(params: Dict[str, Any]) -> Dict[str, Any]:
    content = params.get("content")
    if content is None:
        raise ValueError("parse_content: 'content' is required")
    filename = params.get("filename") or "<inline.py>"
    mod, err = _load_crewai()
    if mod is None:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(f"crewai is not importable: {err}")
    try:
        user_module = _import_user_source(content, filename)
    except ModuleNotFoundError as exc:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(_missing_dep_message(filename, exc))
    except Exception:  # noqa: BLE001 — symmetry with parse_file
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise
    crew = _find_crew(user_module)
    if crew is None:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        return _build_graph(types.SimpleNamespace(agents=[], tasks=[]), filename)
    runtime_graph = _build_graph(crew, filename)
    if not runtime_graph["nodes"]:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
    return runtime_graph


# ----- AST-based fallback parser (ADR-014) ----------------------------------
# CrewAI's modern @CrewBase + @start/@listen Flow patterns construct Crew /
# Agent / Task instances inside instance methods. The runtime path captures
# class instantiation, but Flow files often build at @start/@listen
# boundaries which are too dynamic to safely call. AST fallback walks the
# syntax tree for Agent(role="...") / Task(description="...") / Crew(...)
# constructor calls regardless of containing function.

import ast as _ast


def _try_ast_extract(
    *,
    path: Optional[str] = None,
    content: Optional[str] = None,
    source_path: str,
) -> Optional[Dict[str, Any]]:
    try:
        if content is None:
            with open(path, encoding="utf-8") as fp:
                content = fp.read()
        tree = _ast.parse(content, filename=source_path)
    except (OSError, SyntaxError, UnicodeDecodeError):
        return None
    visitor = _CrewASTVisitor(source_path)
    visitor.visit(tree)
    return visitor.build()


class _CrewASTVisitor(_ast.NodeVisitor):
    """Walks a Python AST collecting Crew/Agent/Task constructor calls."""

    def __init__(self, source_path: str) -> None:
        self._agents: Dict[str, Dict[str, Any]] = {}   # role → node dict
        self._tasks: list[Dict[str, Any]] = []          # ordered list
        self._tools: Dict[str, Dict[str, Any]] = {}     # tool_name → node
        self._edges: list[Dict[str, Any]] = []
        self._source_path = source_path

    @staticmethod
    def _str_arg(node: _ast.expr) -> Optional[str]:
        if isinstance(node, _ast.Constant) and isinstance(node.value, str):
            return node.value
        return None

    @staticmethod
    def _kw_str(call: _ast.Call, *names: str) -> Optional[str]:
        for kw in call.keywords:
            if kw.arg in names:
                v = _CrewASTVisitor._str_arg(kw.value)
                if v is not None:
                    return v
        return None

    @staticmethod
    def _call_name(call: _ast.Call) -> str:
        if isinstance(call.func, _ast.Name):
            return call.func.id
        if isinstance(call.func, _ast.Attribute):
            return call.func.attr
        return ""

    def visit_Call(self, node: _ast.Call) -> None:
        name = self._call_name(node)
        if name == "Agent":
            self._handle_agent(node)
        elif name == "Task":
            self._handle_task(node)
        # Crew(...) itself: we let agents/tasks accumulate; the
        # final graph is assembled in build().
        self.generic_visit(node)

    def _handle_agent(self, node: _ast.Call) -> None:
        role = self._kw_str(node, "role") or ""
        if not role:
            return
        agent_id = "crew::agent::" + _safe_id(role)
        if agent_id in self._agents:
            return
        cfg: Dict[str, Any] = {"agent_role": role}
        # Optional metadata.
        for n in ("goal", "backstory"):
            v = self._kw_str(node, n)
            if v:
                cfg[n] = v
        # Detect allow_delegation=True → mark for circular_dep_agents
        for kw in node.keywords:
            if kw.arg == "allow_delegation" and isinstance(kw.value, _ast.Constant):
                cfg["allow_delegation"] = bool(kw.value.value)
        self._agents[agent_id] = {
            "id":     agent_id,
            "name":   role,
            "type":   "llm",
            "config": cfg,
            "pos":    {"file": self._source_path, "line": getattr(node, "lineno", 0), "col": 0},
        }

    def _handle_task(self, node: _ast.Call) -> None:
        desc = self._kw_str(node, "description") or f"task_{len(self._tasks)}"
        idx = len(self._tasks)
        label = f"{desc[:60]}-{idx}"
        task_id = "crew::task::" + _safe_id(label)
        cfg: Dict[str, Any] = {"description": desc}
        v = self._kw_str(node, "expected_output")
        if v:
            cfg["expected_output"] = v
        node_dict = {
            "id":     task_id,
            "name":   label[:80],
            "type":   "tool",
            "config": cfg,
            "pos":    {"file": self._source_path, "line": getattr(node, "lineno", 0), "col": 0},
        }
        self._tasks.append(node_dict)

    def build(self) -> Dict[str, Any]:
        out_nodes = list(self._agents.values()) + self._tasks
        edges: list[Dict[str, Any]] = []
        # Sequential link Tasks head-to-tail (default Process for AST mode).
        for i in range(len(self._tasks) - 1):
            edges.append({"from": self._tasks[i]["id"], "to": self._tasks[i + 1]["id"]})
        # If multiple agents have allow_delegation=True, emit bidirectional
        # delegate edges so circular_dep_agents fires.
        delegating = [a["id"] for a in self._agents.values()
                      if a["config"].get("allow_delegation")]
        for i, src in enumerate(delegating):
            for dst in delegating[i + 1:]:
                edges.append({"from": src, "to": dst, "condition": "delegate"})
                edges.append({"from": dst, "to": src, "condition": "delegate"})
        entry = self._tasks[0]["id"] if self._tasks else (
            next(iter(self._agents.values()))["id"] if self._agents else ""
        )
        return {
            "nodes": out_nodes,
            "edges": edges,
            "entry_node_id": entry,
            "metadata": {
                "source_format":           "crewai",
                "source_file":             self._source_path,
                "extraction":              "ast_fallback",
                "conditional_edge_reason": "exact_static_match",
            },
        }


def _missing_dep_message(source: str, exc: ModuleNotFoundError) -> str:
    """Format a friendlier error pointing the user at `pip install <name>`.

    The CrewAI parser executes the user's module to introspect Crew/Agent/
    Task instances at runtime (per ADR-013). When the module imports a
    third-party library that isn't installed in the analysis environment,
    Python raises ModuleNotFoundError; the default message points at the
    shim's stack trace which isn't actionable. This wraps it with the
    package name + suggested fix.
    """
    name = getattr(exc, "name", None) or str(exc)
    return (
        f"missing python dependency while parsing {source!r}: "
        f"`import {name}` failed. Run `pip install {name}` (or use a "
        f"virtualenv that has all of the workflow's runtime deps). "
        f"Shingan executes the module to introspect Crew/Agent/Task "
        f"objects, so every transitive import must be installed."
    )


_HANDLERS = {
    "health_check":  _handle_health_check,
    "parse_file":    _handle_parse_file,
    "parse_content": _handle_parse_content,
}


def _dispatch(req: Dict[str, Any]) -> Optional[Dict[str, Any]]:
    req_id = req.get("id")
    method = req.get("method")
    params = req.get("params") or {}
    if method == "shutdown":
        _emit({"id": req_id, "result": {"ok": True}})
        sys.exit(0)
    handler = _HANDLERS.get(method or "")
    if handler is None:
        return _err(req_id, -32601, f"unknown method {method!r}")
    try:
        result = handler(params)
        return {"id": req_id, "result": result}
    except Exception as exc:  # noqa: BLE001
        tb = traceback.format_exc(limit=8)
        return _err(req_id, -32000, f"{type(exc).__name__}: {exc}", data={"traceback": tb})


def main() -> int:
    for raw in sys.stdin:
        line = raw.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
        except json.JSONDecodeError as exc:
            _emit(_err(None, -32700, f"parse error: {exc}"))
            continue
        if not isinstance(req, dict):
            _emit(_err(None, -32600, "invalid request: top-level must be an object"))
            continue
        response = _dispatch(req)
        if response is not None:
            _emit(response)
    return 0


if __name__ == "__main__":
    sys.exit(main())
