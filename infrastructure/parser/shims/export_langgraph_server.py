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
def _package_root(path: str) -> str:
    """Return the first ancestor of `path` that is *not* a Python package.

    A directory is considered a package when it contains `__init__.py`.
    Walking up and stopping at the first non-package gives us the directory
    Python's import system needs on `sys.path` for both relative
    (`from .helpers import X`) and absolute (`from <pkg> import Y`) imports
    inside the user's package to resolve. Real-world OSS (e.g.
    `langgraph_supervisor/supervisor.py`, `legacy/graph.py`) almost always
    sits inside a package — without this, `from <pkg> import …` raises
    ModuleNotFoundError on the package's own name.
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
    """Compute the dotted module name for ``path`` relative to pkg_root.

    Real-world LangGraph / CrewAI files (e.g.
    `multi_agents/agents/orchestrator.py` in gpt-researcher) sit several
    package levels deep and use relative imports (`from .utils.views`,
    `from ..memory.research`). For relative imports to resolve, the
    module needs to be loaded under its REAL dotted name with parent
    packages registered. Falling back to a synthetic name like
    `_shingan_user_<encoded>` causes ImportError on every relative
    import.
    """
    abs_path = os.path.abspath(path)
    rel = os.path.relpath(abs_path, pkg_root)
    parts = rel.replace(os.sep, "/").split("/")
    if parts[-1].endswith(".py"):
        parts[-1] = parts[-1][:-3]
    if parts[-1] == "__init__":
        parts = parts[:-1]
    parts = [p for p in parts if p]
    if not parts:
        # Edge case: pkg_root IS the file's dir (no __init__.py
        # ancestors). Fall back to a synthetic-but-stable name.
        return "_shingan_user_" + abs_path.replace(os.sep, "_")
    return ".".join(parts)


def _is_self_or_parent(missing: str, dotted: str) -> bool:
    """Return True when `missing` is the same as `dotted` or a prefix of
    it. Used to distinguish "package layout problem" (try fallback
    loader) from "third-party dep gap" (surface to user).
    """
    if missing == dotted:
        return True
    return dotted.startswith(missing + ".")


def _register_parent_packages(dotted: str, pkg_root: str) -> list[str]:
    """Register stub `types.ModuleType` entries in sys.modules for every
    parent package of ``dotted``, so relative imports inside the loaded
    module find their parent package context. Returns the list of names
    inserted so the caller can clean up.
    """
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
    """Load ``path`` so relative imports + parent `__init__.py` side
    effects work (e.g. `from . import WriterAgent` requires the parent
    package's __init__.py to execute first and populate its namespace).

    Strategy: prefer `importlib.import_module(dotted)` because Python's
    import system handles the parent chain (executing each
    `__init__.py` in order) automatically. Fall back to
    `spec_from_file_location` only when import_module raises (e.g.
    file is not actually under a package, or path differs from the
    canonical install location).

    Steps:
      1. Walk to the package root (first ancestor without `__init__.py`)
         and prepend it + the file's dir to `sys.path`.
      2. chdir to the file's parent during exec so relative file I/O
         (`open('config/x.yaml')`) finds files alongside the script.
      3. Try `importlib.import_module(dotted)`. If it succeeds, return.
      4. Fall back to `spec_from_file_location` with manually-registered
         parent stubs for files outside any package.
      5. Clean everything up on exit.
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

        # Path 1: dotted import via Python's normal import system.
        # This executes parent __init__.py files so relative imports
        # like `from . import WriterAgent` find the populated parent.
        if "." in dotted and not dotted.startswith("_shingan_user_"):
            try:
                return importlib.import_module(dotted)
            except ModuleNotFoundError as exc:
                # Distinguish "user's own module path can't be located"
                # (fall through to spec_from_file_location) from
                # "transitive third-party dep missing" (re-raise so the
                # `_missing_dep_message` wrapper surfaces the actual
                # package name like `bs4` / `langchain_openai` instead of
                # collapsing it into our synthetic stack frame).
                missing = getattr(exc, "name", "") or ""
                if missing and not _is_self_or_parent(missing, dotted):
                    raise
                # Otherwise fall through.
            except ImportError:
                # Other ImportError flavours: try the fallback loader.
                pass

        # Path 2: synthetic load with stub parents (for files outside
        # any package, or when import_module's PYTHONPATH lookup misses
        # the package layout entirely).
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
    """Compile and execute in-memory Python source as a fresh module.

    If `filename` looks like an on-disk path (i.e. not the inline sentinel
    "<inline.py>" / "<inline>"), prepend its parent directory to sys.path
    so sibling imports (e.g. `from .helpers import ...` or
    `from helpers import ...`) resolve against the buffer's real location.
    Without this, the LSP would report `shingan_parse_error` on every
    multi-module workflow, even though the same file analyses cleanly via
    parse_file (Codex iter5 P1).

    The sys.path entry is removed on exit to avoid leaking import paths
    across separate parse_content calls.
    """
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

    Per Codex iter5 P2: the previous implementation collapsed
    "search-web", "search_web", "search/web", "search.web" all onto
    "search_web", causing distinct nodes to silently merge and edges to
    rewire onto the wrong target. We now preserve the original characters
    that LangGraph allows in identifiers — only stripping whitespace at
    the edges and replacing internal whitespace with a single underscore.
    Hyphens, slashes and dots are kept verbatim so distinct names yield
    distinct IDs.

    START / END pass through unchanged so the caller can detect and elide
    them.
    """
    sname = str(name)
    if sname in (LANGGRAPH_START, LANGGRAPH_END):
        return sname
    cleaned = sname.strip()
    # Replace runs of whitespace with a single underscore; everything else
    # (alnum, _, -, /, ., :) remains as-is to preserve semantic distinctions.
    out: list[str] = []
    in_ws = False
    for ch in cleaned:
        if ch.isspace():
            if not in_ws and out:
                out.append("_")
            in_ws = True
        else:
            out.append(ch)
            in_ws = False
    final = "".join(out).strip("_")
    return final or "node"


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
    """Walk module globals to find a StateGraph instance.

    Resolution order:
      1. Module-level StateGraph / compiled graph instances (the original
         "graph = StateGraph(...)" / "graph = builder.compile()" pattern).
      2. **Factory functions that return a StateGraph** — real-world code
         (LangGraph examples, ADK-Go agents, CrewAI crews, …) overwhelmingly
         constructs the graph inside `make_graph()` / `build_graph()` /
         `def graph()` rather than at module top level. We attempt to call
         zero-argument top-level callables and inspect the return value;
         exceptions / side effects are caught so the worker stays alive.

    Returns the underlying StateGraph object or None.
    """
    candidate: Any = None

    # Pass 1: top-level instances (existing behaviour).
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
    if candidate is not None:
        return candidate

    # Pass 2: zero-arg factory functions. We only try functions defined in
    # the user module itself (skip imports), with no required parameters,
    # and we wrap each call in try/except so a broken factory doesn't kill
    # the worker. The first one returning a StateGraph wins.
    for name, value in vars(module).items():
        if not callable(value) or isinstance(value, type):
            continue
        if value is module:
            continue
        # Only consider functions defined in this module to avoid invoking
        # imported helpers (e.g. langgraph internals).
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
        except Exception:  # noqa: BLE001 — factory may need network / API keys
            continue
        if _is_state_graph(result):
            return result
        for attr in ("builder", "graph"):
            inner = getattr(result, attr, None)
            if inner is not None and _is_state_graph(inner):
                return inner

    return None


def _has_only_optional_args(fn: Any) -> bool:
    """Return True when fn has no required positional/keyword arguments.

    Used by `_find_state_graphs` to skip factories that require config
    (LLM model, API key, etc.) — calling those without args would just
    raise TypeError without producing useful structure for the analysis.

    Note: `self` / `cls` are skipped so methods (and class `__init__`s
    with only `*args, **kwargs`) are correctly recognised.
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
        # Even without langgraph installed we can still try AST extraction —
        # it never imports langgraph itself, just walks the syntax tree.
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(f"langgraph is not importable: {err}")
    try:
        user_module = _import_user_module(path)
    except ModuleNotFoundError as exc:
        # Try AST fallback before giving up — the user file may import a
        # third-party dep we can't install but the graph definition is
        # static and inspectable from source.
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(_missing_dep_message(path, exc))
    graph_obj = _find_state_graphs(user_module)
    if graph_obj is None:
        # Pass-3 AST fallback: the user constructed the graph inside an
        # instance method or factory we can't safely call. Walk the
        # syntax tree for `StateGraph(...).add_node(...).add_edge(...)`
        # patterns instead. ADR-014 (AST hybrid strategy).
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        return _build_graph(types.SimpleNamespace(), path)
    runtime_graph = _build_graph(graph_obj, path)
    if not runtime_graph["nodes"]:
        # Runtime extraction yielded nothing usable — try AST.
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
    return runtime_graph


def _handle_parse_content(params: Dict[str, Any]) -> Dict[str, Any]:
    content = params.get("content")
    if content is None:
        raise ValueError("parse_content: 'content' is required")
    filename = params.get("filename") or "<inline.py>"
    mod, err = _load_langgraph()
    if mod is None:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(f"langgraph is not importable: {err}")
    try:
        user_module = _import_user_source(content, filename)
    except ModuleNotFoundError as exc:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise RuntimeError(_missing_dep_message(filename, exc))
    graph_obj = _find_state_graphs(user_module)
    if graph_obj is None:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        return _build_graph(types.SimpleNamespace(), filename)
    runtime_graph = _build_graph(graph_obj, filename)
    if not runtime_graph["nodes"]:
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
    return runtime_graph


# ----- AST-based fallback parser (ADR-014) ----------------------------------
# Real-world LangGraph code overwhelmingly constructs graphs inside instance
# methods (`class.X._create_workflow(self, agents)`) or factories with
# required arguments. The runtime introspection path can't safely call those.
# This AST walker recognises:
#
#   builder = StateGraph(<anything>)
#   builder.add_node("name", fn)
#   builder.add_edge("a", "b")
#   builder.add_edge(START, "x")           # entry sentinel
#   builder.add_edge("y", END)             # dropped
#   builder.add_conditional_edges("z", fn, {"k1": "a", "k2": "b"})
#   builder.set_entry_point("x")
#
# regardless of where they live in the AST. String-literal arguments are
# extracted; non-literal arguments (variables, expressions) become a synthetic
# placeholder so reachability rules still see SOMETHING (over-approximation
# per ADR-008).

import ast as _ast  # placed here so the runtime path doesn't pay for it


def _try_ast_extract(
    *,
    path: Optional[str] = None,
    content: Optional[str] = None,
    source_path: str,
) -> Optional[Dict[str, Any]]:
    """Walk the AST of `path` or `content` extracting StateGraph patterns.

    Returns a WorkflowGraph dict on success, None on parse error. Never
    raises so the runtime path can fall through cleanly.
    """
    try:
        if content is None:
            with open(path, encoding="utf-8") as fp:
                content = fp.read()
        tree = _ast.parse(content, filename=source_path)
    except (OSError, SyntaxError, UnicodeDecodeError):
        return None
    visitor = _StateGraphASTVisitor(source_path)
    visitor.visit(tree)
    return visitor.build()


class _StateGraphASTVisitor(_ast.NodeVisitor):
    """Walks a Python AST collecting StateGraph builder calls."""

    # Constructor names that produce a StateGraph-like instance.
    _GRAPH_CTORS = ("StateGraph", "MessageGraph", "Graph")
    # Sentinels used by langgraph for entry/exit; never become real nodes.
    _SENTINELS = ("START", "END", "__start__", "__end__")

    def __init__(self, source_path: str) -> None:
        # name → True for variables that hold a StateGraph instance.
        self._builder_vars: Dict[str, bool] = {}
        self._nodes: Dict[str, Dict[str, Any]] = {}
        self._edges: list[Dict[str, Any]] = []
        self._entry: str = ""
        self._source_path = source_path
        # Per-call unique counter so multiple `add_node(<runtime_var>, …)`
        # calls in the same file don't all collapse to a single
        # `<dynamic>` ID — that produced false-positive cycle_detection
        # findings on langchain's own factory.py (dogfood) where the
        # collapsed self-loop wasn't real.
        self._dyn_counter: int = 0

    def visit_Assign(self, node: _ast.Assign) -> None:
        # Detect `<name> = StateGraph(...)`.
        if (
            isinstance(node.value, _ast.Call)
            and self._call_name(node.value) in self._GRAPH_CTORS
        ):
            for target in node.targets:
                if isinstance(target, _ast.Name):
                    self._builder_vars[target.id] = True
                elif isinstance(target, _ast.Attribute):
                    # `self.builder = StateGraph(...)` — track the
                    # attribute name (informationally).
                    self._builder_vars[target.attr] = True
        self.generic_visit(node)

    def visit_AnnAssign(self, node: _ast.AnnAssign) -> None:
        if (
            node.value is not None
            and isinstance(node.value, _ast.Call)
            and self._call_name(node.value) in self._GRAPH_CTORS
        ):
            if isinstance(node.target, _ast.Name):
                self._builder_vars[node.target.id] = True
            elif isinstance(node.target, _ast.Attribute):
                self._builder_vars[node.target.attr] = True
        self.generic_visit(node)

    def visit_Call(self, node: _ast.Call) -> None:
        if isinstance(node.func, _ast.Attribute):
            method = node.func.attr
            if self._is_builder_call(node.func.value):
                if method == "add_node":
                    self._handle_add_node(node)
                elif method == "add_edge":
                    self._handle_add_edge(node)
                elif method == "add_conditional_edges":
                    self._handle_add_conditional(node)
                elif method == "set_entry_point":
                    self._handle_set_entry(node)
        self.generic_visit(node)

    def _is_builder_call(self, expr: _ast.expr) -> bool:
        """Return True when `expr` references a StateGraph builder var.

        Handles `builder.add_node(...)`, `self.workflow.add_node(...)`,
        `self.builder.compile().add_node(...)` (we just check the
        immediate name/attr).
        """
        if isinstance(expr, _ast.Name):
            return expr.id in self._builder_vars
        if isinstance(expr, _ast.Attribute):
            return expr.attr in self._builder_vars
        if isinstance(expr, _ast.Call):
            # Chained: `builder.compile().add_node(...)` — recurse.
            return self._is_builder_call(expr.func) if isinstance(
                expr.func, (_ast.Attribute, _ast.Name)
            ) else False
        return False

    @staticmethod
    def _call_name(call: _ast.Call) -> str:
        if isinstance(call.func, _ast.Name):
            return call.func.id
        if isinstance(call.func, _ast.Attribute):
            return call.func.attr
        return ""

    @staticmethod
    def _str_arg(node: _ast.expr) -> Optional[str]:
        """Extract a string literal value, or None for non-literal args."""
        if isinstance(node, _ast.Constant) and isinstance(node.value, str):
            return node.value
        return None

    @staticmethod
    def _sentinel_arg(node: _ast.expr) -> Optional[str]:
        """Detect START / END references regardless of import alias."""
        if isinstance(node, _ast.Name) and node.id in _StateGraphASTVisitor._SENTINELS:
            return node.id
        if isinstance(node, _ast.Attribute) and node.attr in _StateGraphASTVisitor._SENTINELS:
            return node.attr
        if isinstance(node, _ast.Constant) and node.value in _StateGraphASTVisitor._SENTINELS:
            return str(node.value)
        return None

    def _ensure_node(self, name: str, ntype: str = "llm") -> None:
        if name in self._nodes:
            return
        self._nodes[name] = {
            "id":     name,
            "name":   name,
            "type":   ntype,
            "config": {},
            "pos":    {"file": self._source_path, "line": 0, "col": 0},
        }

    def _node_type_for(self, fn: _ast.expr) -> str:
        """Heuristic: handler argument's name → tool/llm classification."""
        name = ""
        if isinstance(fn, _ast.Name):
            name = fn.id
        elif isinstance(fn, _ast.Attribute):
            name = fn.attr
        elif isinstance(fn, _ast.Lambda):
            name = "lambda"
        lower = name.lower()
        if any(t in lower for t in ("tool", "retriev", "search", "fetch", "browser")):
            return "tool"
        return "llm"

    def _handle_add_node(self, node: _ast.Call) -> None:
        if not node.args:
            return
        # add_node("name", fn) OR add_node(fn) (1-arg sugar where fn.__name__
        # is the name in newer langgraph). Non-literal name args get a
        # PER-CALL unique placeholder so we don't collapse N distinct
        # `add_node(state.var, fn_n)` calls onto one `<dynamic>` node and
        # invent self-cycles between them.
        if len(node.args) >= 2:
            name = self._str_arg(node.args[0]) or self._next_dynamic_name()
            ntype = self._node_type_for(node.args[1])
            self._ensure_node(name, ntype)
        else:
            # Single-arg form: callable's name.
            fn = node.args[0]
            name = ""
            if isinstance(fn, _ast.Name):
                name = fn.id
            elif isinstance(fn, _ast.Attribute):
                name = fn.attr
            if not name:
                name = self._next_dynamic_name()
            self._ensure_node(name, self._node_type_for(fn))

    def _next_dynamic_name(self) -> str:
        self._dyn_counter += 1
        return f"<dynamic_{self._dyn_counter}>"

    def _handle_add_edge(self, node: _ast.Call) -> None:
        if len(node.args) < 2:
            return
        src_arg, dst_arg = node.args[0], node.args[1]
        src = self._sentinel_arg(src_arg) or self._str_arg(src_arg)
        dst = self._sentinel_arg(dst_arg) or self._str_arg(dst_arg)
        # Skip edges whose endpoints aren't string literals — we can't
        # know what they connect, and inventing a synthetic placeholder
        # creates false-positive cycles (langchain factory.py dogfood).
        if src is None or dst is None:
            return
        if src in self._SENTINELS:
            # START → user-node: the user-node becomes the entry. Drop the edge.
            if dst not in self._SENTINELS and not self._entry:
                self._entry = dst
                self._ensure_node(dst, "llm")
            return
        if dst in self._SENTINELS:
            # User-node → END: drop the edge entirely.
            self._ensure_node(src, "llm")
            return
        self._ensure_node(src, "llm")
        self._ensure_node(dst, "llm")
        self._edges.append({"from": src, "to": dst})

    def _handle_add_conditional(self, node: _ast.Call) -> None:
        if len(node.args) < 2:
            return
        src_arg = node.args[0]
        src = self._sentinel_arg(src_arg) or self._str_arg(src_arg)
        # Skip when the source isn't a string literal — same false-cycle
        # rationale as _handle_add_edge.
        if src is None:
            return
        if src in self._SENTINELS:
            return
        self._ensure_node(src, "llm")
        # Try to extract the {key: target_name} mapping from the third positional
        # arg (or `path_map`/`mapping` keyword).
        mapping = None
        if len(node.args) >= 3 and isinstance(node.args[2], _ast.Dict):
            mapping = node.args[2]
        for kw in node.keywords:
            if kw.arg in ("path_map", "mapping") and isinstance(kw.value, _ast.Dict):
                mapping = kw.value
                break
        if mapping is None:
            return
        for key, value in zip(mapping.keys, mapping.values):
            cond = self._str_arg(key) if key is not None else ""
            target = self._sentinel_arg(value) or self._str_arg(value)
            if not target:
                continue
            if target in self._SENTINELS:
                continue
            self._ensure_node(target, "llm")
            edge = {"from": src, "to": target}
            if cond:
                edge["condition"] = cond
            self._edges.append(edge)

    def _handle_set_entry(self, node: _ast.Call) -> None:
        if not node.args:
            return
        name = self._str_arg(node.args[0])
        if name and not self._entry:
            self._entry = name
            self._ensure_node(name, "llm")

    def build(self) -> Dict[str, Any]:
        # Drop synthetic placeholder nodes if a real entry exists; otherwise
        # keep them so over-approximation surfaces.
        out_nodes = list(self._nodes.values())
        entry = self._entry
        if not entry and out_nodes:
            entry = out_nodes[0]["id"]
        return {
            "nodes": out_nodes,
            "edges": self._edges,
            "entry_node_id": entry,
            "metadata": {
                "source_format":           "langgraph",
                "source_file":             self._source_path,
                "extraction":              "ast_fallback",
                "conditional_edge_reason": "over_approximated_dynamic",
            },
        }


def _missing_dep_message(source: str, exc: ModuleNotFoundError) -> str:
    """Format a friendlier error pointing the user at `pip install <name>`.

    The LangGraph parser executes the user's module to introspect the
    StateGraph instance at runtime. When the module imports a
    third-party library that isn't installed in the analysis environment,
    Python raises ModuleNotFoundError; the default message buries it in
    a stack trace. This wraps it with the package name + suggested fix.
    """
    name = getattr(exc, "name", None) or str(exc)
    return (
        f"missing python dependency while parsing {source!r}: "
        f"`import {name}` failed. Run `pip install {name}` (or use a "
        f"virtualenv that has all of the workflow's runtime deps). "
        f"Shingan executes the module to introspect StateGraph objects, "
        f"so every transitive import must be installed."
    )


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
