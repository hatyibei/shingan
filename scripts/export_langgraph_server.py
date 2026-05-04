#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Long-lived JSON-RPC worker that exports LangGraph StateGraph definitions
into Shingan's WorkflowGraph JSON format.

Protocol
========
Newline-delimited JSON (one request per line on stdin, one response per line
on stdout). All log output, langgraph warnings, and tracebacks go to stderr —
stdout MUST contain only response frames.

Request:
    {"id": <int|str>, "method": "<name>", "params": {...}}

Success response:
    {"id": <id>, "result": <any>}

Error response:
    {"id": <id>, "error": {"code": <int>, "message": "<text>"}}

Methods
-------
- ``health_check``  → ``{"status": "ok", "langgraph_version": "x.y.z"}``
                     or ``{"status": "missing_langgraph", ...}`` (worker stays
                     alive so the Go side can produce a structured error).
- ``parse_file``    → ``{"path": <abs path>}`` → WorkflowGraph JSON
- ``parse_content`` → ``{"content": <python source>, "filename": <hint>}``
                     → WorkflowGraph JSON
- ``shutdown``      → ``{"ok": true}`` then process exits.

Compatible with ``langgraph >= 0.2`` but tolerant of API drift: missing private
attributes degrade to an empty graph rather than crashing the worker.

ADR references
--------------
- ADR-009: long-lived worker (no per-file fork) + degraded mode.
- ADR-008: ``over_approximated_dynamic`` ConfidenceReason for conditional edges
  whose mapping is statically resolvable but represents a runtime branch.
- ADR-011: LangGraph as Phase 1 primary parser.
"""
from __future__ import annotations

import importlib
import importlib.util
import inspect
import json
import os
import sys
import traceback
import types
from typing import Any, Dict, Optional, Tuple

# ----- stdout/stderr discipline ---------------------------------------------
# `langgraph` prints deprecation warnings and other chatter on import. Re-route
# all "natural" stdout to stderr so that response framing on stdout stays clean.
_RESPONSE_STREAM = sys.stdout
sys.stdout = sys.stderr  # any stray print() now lands on stderr.

# Sentinel names used by langgraph itself for START / END.
LANGGRAPH_START = "__start__"
LANGGRAPH_END = "__end__"


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


# ----- langgraph loading -----------------------------------------------------
def _load_langgraph() -> Tuple[Optional[types.ModuleType], Optional[str]]:
    try:
        mod = importlib.import_module("langgraph")
    except Exception as exc:  # noqa: BLE001 — we want any import failure
        return None, f"{type(exc).__name__}: {exc}"
    return mod, None


def _langgraph_version() -> str:
    try:
        mod = importlib.import_module("langgraph")
        return getattr(mod, "__version__", "unknown")
    except Exception:  # noqa: BLE001
        return "missing"


# ----- module loader for parse_file / parse_content -------------------------
def _import_user_module(path: str) -> types.ModuleType:
    """Load ``path`` as a fresh Python module without polluting sys.modules.

    The directory containing ``path`` is prepended to sys.path so sibling
    imports work, then restored afterwards.
    """
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
    """Compile and execute in-memory Python source as a fresh module."""
    module = types.ModuleType(f"_shingan_inline_{abs(hash(filename))}")
    module.__file__ = filename or "<inline>"
    code = compile(content, filename or "<inline>", "exec")
    exec(code, module.__dict__)  # noqa: S102 — intentional
    return module


# ----- StateGraph extraction -------------------------------------------------
def _is_state_graph(obj: Any) -> bool:
    """Return True if *obj* looks like a langgraph StateGraph instance.

    We detect by *class name* rather than `isinstance` so the worker still
    parses correctly when langgraph is unavailable in the import environment
    (e.g. user shipped a stub). Real langgraph rules go through the same path.
    """
    cls = type(obj)
    if cls.__name__ in {"StateGraph", "Graph", "MessageGraph"}:
        return True
    # Walk MRO for type hierarchies (StateGraph subclasses).
    for base in cls.__mro__:
        if base.__name__ in {"StateGraph", "Graph", "MessageGraph"}:
            return True
    return False


def _source_pos_for(fn: Any, fallback_path: str) -> Dict[str, Any]:
    """Best-effort SourcePos for a callable / class associated with a node."""
    try:
        path = inspect.getsourcefile(fn) or inspect.getfile(fn) or fallback_path
        try:
            _, lineno = inspect.getsourcelines(fn)
        except (OSError, TypeError):
            lineno = 0
        return {"file": path, "line": lineno, "col": 0}
    except (TypeError, OSError):
        return {"file": fallback_path, "line": 0, "col": 0}


def _node_type_for_handler(handler: Any) -> str:
    """Map a node's handler/runnable to a Shingan NodeType string.

    Heuristics (Phase 1 — more granular detection lands when ADR-007 ships):
      * Anything obviously RunnableSequence/Tool/RetrieverInterface → ``tool``
      * Callable whose qualname contains ``llm`` / ``chat`` / ``model`` → ``llm``
      * Otherwise → ``llm`` (LangGraph nodes are usually agent calls)
    """
    name = ""
    try:
        name = (
            getattr(handler, "name", None)
            or getattr(handler, "__name__", "")
            or type(handler).__name__
        )
    except Exception:  # noqa: BLE001
        pass
    lower = (name or "").lower()
    if any(tok in lower for tok in ("tool", "retriev", "search", "fetch", "browser")):
        return "tool"
    return "llm"


def _read_field(obj: Any, *names: str) -> Any:
    for n in names:
        if hasattr(obj, n):
            return getattr(obj, n)
    return None


def _extract_nodes(graph: Any) -> Dict[str, Any]:
    """Pull the {name: NodeSpec} dict from a StateGraph in API-tolerant fashion."""
    nodes = _read_field(graph, "nodes")
    if isinstance(nodes, dict):
        return nodes
    # Some langgraph versions use ``_nodes``/``__nodes``.
    private = _read_field(graph, "_nodes", "__nodes")
    if isinstance(private, dict):
        return private
    return {}


def _extract_edges(graph: Any) -> Any:
    """Return edges container in whichever form langgraph happens to use."""
    return _read_field(graph, "edges", "_edges", "__edges")


def _extract_branches(graph: Any) -> Dict[str, Any]:
    """Return branches dict (for conditional_edges); empty if none."""
    branches = _read_field(graph, "branches", "_branches", "__branches")
    if isinstance(branches, dict):
        return branches
    return {}


def _extract_entry_point(graph: Any) -> Optional[str]:
    return _read_field(graph, "entry_point", "_entry_point", "__entry_point")


def _node_handler(spec: Any) -> Any:
    """Pull the user-supplied callable out of a NodeSpec / runnable / dict."""
    if spec is None:
        return None
    if callable(spec):
        return spec
    for attr in ("runnable", "fn", "func", "callable", "node"):
        cand = getattr(spec, attr, None)
        if cand is not None:
            return cand
    if isinstance(spec, dict):
        for k in ("runnable", "fn", "func", "callable", "node"):
            if k in spec:
                return spec[k]
    return spec


def _normalise_edge(edge: Any) -> Optional[Tuple[str, str]]:
    """Coerce miscellaneous edge representations into ``(from, to)`` tuples."""
    if isinstance(edge, tuple) and len(edge) == 2:
        return str(edge[0]), str(edge[1])
    if isinstance(edge, dict):
        a = edge.get("source") or edge.get("from") or edge.get("start")
        b = edge.get("target") or edge.get("to") or edge.get("end")
        if a and b:
            return str(a), str(b)
    a = getattr(edge, "source", None) or getattr(edge, "from_", None) or getattr(edge, "start", None)
    b = getattr(edge, "target", None) or getattr(edge, "to", None) or getattr(edge, "end", None)
    if a and b:
        return str(a), str(b)
    return None


def _normalise_branch(branch: Any) -> Dict[str, Any]:
    """Pull a conditional-edge branch into a {ends: dict, path: callable} dict.

    Returns an empty dict if the structure is unrecognisable.
    """
    if isinstance(branch, dict):
        return branch
    out: Dict[str, Any] = {}
    for attr in ("ends", "mapping", "path_map"):
        v = getattr(branch, attr, None)
        if isinstance(v, dict):
            out["ends"] = v
            break
    for attr in ("path", "fn", "callable"):
        v = getattr(branch, attr, None)
        if v is not None and "path" not in out:
            out["path"] = v
    return out


# ----- WorkflowGraph builder -------------------------------------------------
def _node_id(name: str) -> str:
    """Map a langgraph node name to a Shingan node ID.

    LangGraph allows arbitrary strings (including spaces). We lower-case and
    swap whitespace/hyphens for underscores. START / END pass through
    unchanged so the caller can detect and elide them.
    """
    sname = str(name)
    if sname in (LANGGRAPH_START, LANGGRAPH_END):
        return sname
    out = []
    for ch in sname:
        if ch.isalnum() or ch == "_":
            out.append(ch.lower())
        elif ch in (" ", "-", "/", "."):
            out.append("_")
    cleaned = "".join(out).strip("_")
    return cleaned or "node"


def _build_graph(graph_obj: Any, source_path: str) -> Dict[str, Any]:
    """Convert a StateGraph into Shingan WorkflowGraph JSON shape.

    START / END are *not* materialised as nodes — LangGraph treats them as
    pseudo-sentinels rather than first-class nodes, and Shingan's analysis
    rules misclassify them otherwise (NodeTypeControl in JSON aliases to
    NodeTypeLoop for backward compat, which would trigger spurious
    `loop_guard` Critical findings). Instead:

      * the first edge sourced from `__start__` becomes the graph's entry,
      * any edge targeting `__end__` is dropped (the entry node is the
        graph's source-of-truth, not a synthetic sink).
    """
    out_nodes = []
    out_edges = []

    raw_nodes = _extract_nodes(graph_obj)
    raw_edges = _extract_edges(graph_obj)
    raw_branches = _extract_branches(graph_obj)
    entry = _extract_entry_point(graph_obj)

    seen_ids: Dict[str, bool] = {}

    def _ensure_node(node_id: str, name: str, ntype: str, pos: Dict[str, Any], cfg: Dict[str, Any]) -> None:
        if node_id in seen_ids:
            return
        seen_ids[node_id] = True
        out_nodes.append(
            {
                "id": node_id,
                "name": name,
                "type": ntype,
                "config": cfg,
                "pos": pos,
            }
        )

    # User-defined nodes only — START / END are sentinels, never materialised.
    for name, spec in raw_nodes.items():
        if str(name) in (LANGGRAPH_START, LANGGRAPH_END):
            continue
        handler = _node_handler(spec)
        node_id = _node_id(name)
        ntype = _node_type_for_handler(handler)
        pos = _source_pos_for(handler, source_path) if handler is not None else {
            "file": source_path,
            "line": 0,
            "col": 0,
        }
        cfg: Dict[str, Any] = {}
        # Surface the handler qualname for downstream reporting.
        try:
            cfg["handler"] = getattr(handler, "__qualname__", None) or getattr(
                handler, "__name__", None
            ) or type(handler).__name__
        except Exception:  # noqa: BLE001
            pass
        _ensure_node(node_id, str(name), ntype, pos, cfg)

    # Track entry candidates (first edge sourced from START wins).
    entry_node_id: str = ""

    def _record_entry(target_id: str) -> None:
        nonlocal entry_node_id
        if not entry_node_id:
            entry_node_id = target_id

    def _push_edge(src: str, dst: str, condition: str = "") -> None:
        # START → user-node: mark entry, drop the edge.
        if src == LANGGRAPH_START:
            _record_entry(dst)
            return
        # User-node → END: drop the edge entirely (END is implicit).
        if dst == LANGGRAPH_END:
            return
        # Defensive: skip START/END appearing on the wrong side too.
        if src == LANGGRAPH_END or dst == LANGGRAPH_START:
            return
        edge: Dict[str, Any] = {"from": src, "to": dst}
        if condition:
            edge["condition"] = condition
        out_edges.append(edge)

    # Static edges.
    edges_iter: Any
    if isinstance(raw_edges, (set, list, tuple)):
        edges_iter = list(raw_edges)
    elif isinstance(raw_edges, dict):
        edges_iter = []
        for src, dsts in raw_edges.items():
            if isinstance(dsts, (list, tuple, set)):
                for d in dsts:
                    edges_iter.append((src, d))
            else:
                edges_iter.append((src, dsts))
    else:
        edges_iter = []

    for raw_edge in edges_iter:
        pair = _normalise_edge(raw_edge)
        if pair is None:
            continue
        src, dst = pair
        _push_edge(_node_id(src), _node_id(dst))

    # Conditional branches → over-approximated edges.
    # Each branch maps source → list of (return-value → target-name) entries.
    for src, branch_set in (raw_branches or {}).items():
        if isinstance(branch_set, dict):
            branches = list(branch_set.values())
        elif isinstance(branch_set, (list, tuple, set)):
            branches = list(branch_set)
        else:
            branches = [branch_set]
        for branch in branches:
            normalised = _normalise_branch(branch)
            ends = normalised.get("ends") or {}
            if isinstance(ends, dict):
                items = ends.items()
            elif isinstance(ends, (list, tuple, set)):
                items = [(str(t), t) for t in ends]
            else:
                items = []
            for cond_key, target in items:
                if not target:
                    continue
                _push_edge(_node_id(src), _node_id(str(target)), str(cond_key))

    # Honour an explicit entry_point if `add_edge(START, x)` wasn't recorded.
    if not entry_node_id and entry:
        entry_node_id = _node_id(str(entry))

    # Final fallback — pick the first user node so downstream rules have a
    # well-defined starting point.
    if not entry_node_id and out_nodes:
        entry_node_id = out_nodes[0]["id"]

    return {
        "nodes": out_nodes,
        "edges": out_edges,
        "entry_node_id": entry_node_id,
        # `metadata` is non-canonical but parsers may surface this for tools
        # downstream; the Go layer ignores unknown keys.
        "metadata": {
            "source_format": "langgraph",
            "source_file": source_path,
            "langgraph_version": _langgraph_version(),
            "conditional_edge_reason": "over_approximated_dynamic",
        },
    }


def _find_state_graphs(module: types.ModuleType) -> Any:
    """Walk module globals to find the first StateGraph instance.

    We look for *compiled* graphs first (``module.graph = builder.compile()``)
    by checking ``builder`` attribute, then fall back to plain StateGraph
    instances. Returns the underlying StateGraph object or None.
    """
    candidate: Any = None
    for _name, value in vars(module).items():
        if value is module:
            continue
        if _is_state_graph(value):
            return value
        # Compiled graphs expose .builder (the StateGraph) in recent langgraph.
        builder = getattr(value, "builder", None)
        if builder is not None and _is_state_graph(builder):
            candidate = builder
            return candidate
        # Older API: .graph attribute.
        graph = getattr(value, "graph", None)
        if graph is not None and _is_state_graph(graph):
            return graph
    return candidate


# ----- handlers --------------------------------------------------------------
def _handle_health_check(_: Dict[str, Any]) -> Dict[str, Any]:
    mod, err = _load_langgraph()
    if mod is None:
        return {"status": "missing_langgraph", "error": err, "python": sys.version}
    return {
        "status": "ok",
        "langgraph_version": getattr(mod, "__version__", "unknown"),
        "python": sys.version,
    }


def _handle_parse_file(params: Dict[str, Any]) -> Dict[str, Any]:
    path = params.get("path")
    if not path:
        raise ValueError("parse_file: 'path' is required")
    mod, err = _load_langgraph()
    if mod is None:
        raise RuntimeError(f"langgraph is not importable: {err}")
    user_module = _import_user_module(path)
    graph_obj = _find_state_graphs(user_module)
    if graph_obj is None:
        # Empty but valid graph — keeps Shingan rules working without exploding.
        return _build_graph(types.SimpleNamespace(), path)
    return _build_graph(graph_obj, path)


def _handle_parse_content(params: Dict[str, Any]) -> Dict[str, Any]:
    content = params.get("content")
    if content is None:
        raise ValueError("parse_content: 'content' is required")
    filename = params.get("filename") or "<inline.py>"
    mod, err = _load_langgraph()
    if mod is None:
        raise RuntimeError(f"langgraph is not importable: {err}")
    user_module = _import_user_source(content, filename)
    graph_obj = _find_state_graphs(user_module)
    if graph_obj is None:
        return _build_graph(types.SimpleNamespace(), filename)
    return _build_graph(graph_obj, filename)


_HANDLERS = {
    "health_check": _handle_health_check,
    "parse_file": _handle_parse_file,
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
    except Exception as exc:  # noqa: BLE001 — surface every error as a frame
        tb = traceback.format_exc(limit=8)
        return _err(req_id, -32000, f"{type(exc).__name__}: {exc}", data={"traceback": tb})


def main() -> int:
    # Read newline-delimited JSON from stdin until EOF or shutdown.
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
