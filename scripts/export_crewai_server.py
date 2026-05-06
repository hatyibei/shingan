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
    return mod, None


def _crewai_version() -> str:
    try:
        mod = importlib.import_module("crewai")
        return getattr(mod, "__version__", "unknown")
    except Exception:  # noqa: BLE001
        return "missing"


# ----- module loader ---------------------------------------------------------
def _import_user_module(path: str) -> types.ModuleType:
    abs_path = os.path.abspath(path)
    src_dir = os.path.dirname(abs_path)
    inserted = False
    if src_dir and src_dir not in sys.path:
        sys.path.insert(0, src_dir)
        inserted = True
    try:
        spec = importlib.util.spec_from_file_location(
            f"_shingan_user_{abs_path.replace(os.sep, '_')}", abs_path
        )
        if spec is None or spec.loader is None:
            raise ImportError(f"could not load {abs_path}")
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)  # type: ignore[union-attr]
        return module
    finally:
        if inserted:
            try:
                sys.path.remove(src_dir)
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
      * Process.hierarchical → manager → every worker → manager (over-approx)
      * Agent.tools[t]    → Edge(agent_id → tool_id, condition="uses_tool")
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

    for agent in agents:
        if not _is_agent(agent):
            continue
        role = _stringify(_read(agent, "role", default="agent"), max_len=120) or "agent"
        a_id = _agent_id(crew_id, role)
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
    tool_seen: Dict[str, bool] = {}
    for a_id, tool, tname in agent_tools:
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
                # Pydantic schemas → just the class name; full schema goes in
                # via model_json_schema() if downstream rules need it.
                tool_cfg["args_schema"] = type(args).__name__
            out_nodes.append({
                "id":     t_id,
                "name":   tname,
                "type":   "tool",
                "config": tool_cfg,
                "pos":    {"file": source_path, "line": 0, "col": 0},
            })
        out_edges.append({"from": a_id, "to": t_id, "condition": "uses_tool"})

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
        # by index so edges land on the right destination.
        t_label = f"{desc[:60]}#{i}" if desc else f"task_{i}"
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
                out_edges.append({"from": t_id, "to": assigned, "condition": "uses_agent"})
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
        # Find the manager LLM / agent.
        manager_id = ""
        manager_llm = _read(crew, "manager_llm")
        if manager_llm is not None:
            mname = _stringify(_read(manager_llm, "model_name", "model",
                                     default=type(manager_llm).__name__),
                                max_len=80) or "manager_llm"
            manager_id = _agent_id(crew_id, "manager_" + mname)
            if manager_id not in seen:
                seen[manager_id] = True
                mcfg: Dict[str, Any] = {
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
                out_edges.append({"from": manager_id, "to": worker_id, "condition": "delegate"})
                out_edges.append({"from": worker_id, "to": manager_id, "condition": "report"})
            # Connect manager into every Task too.
            for tid in task_ids:
                out_edges.append({"from": manager_id, "to": tid, "condition": "manage"})
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
    """Walk module globals to find the first Crew instance.

    The user can declare:
        crew = Crew(agents=[...], tasks=[...])
    or use a factory:
        def make_crew() -> Crew: ...
        crew = make_crew()

    We accept either — finding any module-level value that is_crew().
    """
    for _name, value in vars(module).items():
        if value is module:
            continue
        if _is_crew(value):
            return value
        # Some helper modules expose an attribute (e.g. obj.crew).
        attr = getattr(value, "crew", None)
        if attr is not None and _is_crew(attr):
            return attr
    return None


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
        raise RuntimeError(f"crewai is not importable: {err}")
    user_module = _import_user_module(path)
    crew = _find_crew(user_module)
    if crew is None:
        return _build_graph(types.SimpleNamespace(agents=[], tasks=[]), path)
    return _build_graph(crew, path)


def _handle_parse_content(params: Dict[str, Any]) -> Dict[str, Any]:
    content = params.get("content")
    if content is None:
        raise ValueError("parse_content: 'content' is required")
    filename = params.get("filename") or "<inline.py>"
    mod, err = _load_crewai()
    if mod is None:
        raise RuntimeError(f"crewai is not importable: {err}")
    user_module = _import_user_source(content, filename)
    crew = _find_crew(user_module)
    if crew is None:
        return _build_graph(types.SimpleNamespace(agents=[], tasks=[]), filename)
    return _build_graph(crew, filename)


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
