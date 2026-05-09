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
    """Return edges container, merging the fan-in `waiting_edges` set
    when present.

    LangGraph stores standard 1:1 edges in `.edges` and the
    `add_edge(["a", "b"], "c")` fan-in form in `.waiting_edges`
    (entries shaped `((src1, src2), dst)`). Without merging the two,
    the runtime path treated fan-in joins as missing and the
    downstream join node looked unreachable. Dogfood:
    langgraph/libs/langgraph/bench/wide_state.py.
    """
    edges = _read_field(graph, "edges", "_edges", "__edges")
    waiting = _read_field(graph, "waiting_edges")
    if waiting is None:
        return edges
    merged = []
    if isinstance(edges, (set, list, tuple)):
        merged.extend(edges)
    elif isinstance(edges, dict):
        for s, dsts in edges.items():
            if isinstance(dsts, (list, tuple, set)):
                for d in dsts:
                    merged.append((s, d))
            else:
                merged.append((s, dsts))
    if isinstance(waiting, (set, list, tuple)):
        merged.extend(waiting)
    return merged


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


def _normalise_edge(edge: Any) -> Optional[list[Tuple[str, str]]]:
    """Coerce miscellaneous edge representations into ``[(from, to), …]``.

    Returns a LIST so the LangGraph fan-in form
    `add_edge(["a", "b"], "c")` — internally stored as
    `(("a", "b"), "c")` — expands to multiple `(from, to)` pairs
    rather than producing a single bogus edge with a tuple-string
    source like `"('a', 'b')"`. Single-source edges still come back
    as a length-1 list.
    """
    if isinstance(edge, tuple) and len(edge) == 2:
        a, b = edge
        if isinstance(a, (list, tuple)):
            return [(str(s), str(b)) for s in a]
        return [(str(a), str(b))]
    if isinstance(edge, dict):
        a = edge.get("source") or edge.get("from") or edge.get("start")
        b = edge.get("target") or edge.get("to") or edge.get("end")
        if a and b:
            if isinstance(a, (list, tuple)):
                return [(str(s), str(b)) for s in a]
            return [(str(a), str(b))]
    a = getattr(edge, "source", None) or getattr(edge, "from_", None) or getattr(edge, "start", None)
    b = getattr(edge, "target", None) or getattr(edge, "to", None) or getattr(edge, "end", None)
    if a and b:
        if isinstance(a, (list, tuple)):
            return [(str(s), str(b)) for s in a]
        return [(str(a), str(b))]
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

    def _mark_runtime_end_branch(src_id: str) -> None:
        """Source-node has an explicit END branch. Lets cycle_detection
        recognise the cycle as bounded — see AST visitor's `_mark_end_branch`."""
        for n in out_nodes:
            if n["id"] == src_id:
                n["has_exit_branch"] = True
                n.setdefault("config", {})["has_end_branch"] = True
                return

    def _push_edge(src: str, dst: str, condition: str = "") -> None:
        # START → user-node: mark entry, drop the edge.
        if src == LANGGRAPH_START:
            _record_entry(dst)
            return
        # User-node → END: drop the edge but mark exit on source.
        if dst == LANGGRAPH_END:
            _mark_runtime_end_branch(src)
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
        pairs = _normalise_edge(raw_edge)
        if pairs is None:
            continue
        for src, dst in pairs:
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

    payload = {
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

    # LangGraph's runtime introspection only sees edges declared via
    # `add_edge` / `add_conditional_edges`. Dynamic dispatch via
    # `def fn(...) -> Command[Literal[...]]` or `return Command(goto=...)`
    # bypasses that and shows up only at execution time. We rebuild the
    # AST visitor at this stage purely to harvest its
    # `_command_goto_map` + per-node handler links and synthesise the
    # implicit edges. This keeps runtime-path graphs symmetric with
    # AST-fallback graphs (open_deep_research dogfood: 9 findings → 1).
    _augment_runtime_graph_with_command_goto(payload, source_path)

    return payload


def _augment_runtime_graph_with_command_goto(
    payload: Dict[str, Any], source_path: str,
) -> None:
    """Attach Command(goto=...) / Command[Literal[...]] edges that the
    runtime LangGraph introspection misses.

    Strategy: re-parse the source via `_StateGraphASTVisitor`, take its
    `_command_goto_map` (function-name → destination set) and the
    handlers it tracked per builder, then for every node in `payload`
    whose handler is also recorded in the AST view, add the
    Command-goto edges to the payload (skipping duplicates).
    """
    try:
        if not source_path:
            return
        with open(source_path, encoding="utf-8") as fp:
            content = fp.read()
        tree = _ast.parse(content, filename=source_path)
    except (OSError, SyntaxError, UnicodeDecodeError):
        return
    visitor = _StateGraphASTVisitor(source_path)
    visitor.visit(tree)
    if not visitor._command_goto_map:
        return
    # Combine handler maps from every discovered builder var; the
    # runtime path doesn't tell us which subgraph a node lives in, so
    # we treat all handler→fn associations as eligible.
    handler_to_fn: Dict[str, str] = {}
    for graph_state in visitor._graphs.values():
        for node_name, fn_name in (graph_state.get("handlers") or {}).items():
            handler_to_fn[node_name] = fn_name
    if not handler_to_fn:
        return
    node_ids = {n["id"] for n in payload.get("nodes", [])}
    nodes_by_id = {n["id"]: n for n in payload.get("nodes", [])}
    existing_edges = {(e["from"], e["to"]) for e in payload.get("edges", [])}
    added = 0
    SENTINELS = ("START", "END", "__start__", "__end__")
    for node_id in node_ids:
        fn_name = handler_to_fn.get(node_id)
        if not fn_name:
            continue
        dests = visitor._command_goto_map.get(fn_name)
        if not dests:
            continue
        for dest in sorted(dests):
            if dest in SENTINELS:
                # Sentinel destination → mark source exit, no edge.
                n = nodes_by_id[node_id]
                n["has_exit_branch"] = True
                n.setdefault("config", {})["has_end_branch"] = True
                continue
            if dest not in node_ids:
                continue
            if (node_id, dest) in existing_edges:
                continue
            payload["edges"].append({
                "from":      node_id,
                "to":        dest,
                "condition": "command_goto",
            })
            existing_edges.add((node_id, dest))
            added += 1
    if added:
        payload["metadata"]["command_goto_edges"] = added


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

    Multi-graph priority (matches AST-fallback `_StateGraphASTVisitor`):
      • A CompiledStateGraph at module level (`graph = builder.compile()`)
        is the highest-confidence "main" — the user explicitly chose to
        compile that one. We keep the *last* such variable seen so that
        files defining several subgraphs followed by a compiled outer
        graph (e.g. open_deep_research/legacy/graph.py) resolve to the
        outer one rather than the first inner subgraph.
      • Otherwise fall back to the last bare StateGraph instance.

    Returns the underlying StateGraph object or None.
    """
    last_compiled: Any = None
    last_bare: Any = None

    # Pass 1: top-level instances. Iterate globals fully (Python 3.7+ dict
    # preserves insertion order) so we end up with the *latest* candidate
    # rather than the first match.
    for _name, value in vars(module).items():
        if value is module:
            continue
        # Compiled graphs expose .builder (the StateGraph) in recent langgraph.
        builder = getattr(value, "builder", None)
        if builder is not None and _is_state_graph(builder):
            last_compiled = builder
            continue
        # Older API: .graph attribute on the compiled wrapper.
        compiled_inner = getattr(value, "graph", None)
        if compiled_inner is not None and _is_state_graph(compiled_inner):
            last_compiled = compiled_inner
            continue
        if _is_state_graph(value):
            last_bare = value
    if last_compiled is not None:
        return last_compiled
    if last_bare is not None:
        return last_bare

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
    except Exception:  # noqa: BLE001 — any other import-time failure
        # Modules with side-effects at import (langchain_openai
        # initialising a client, langgraph studio modules instantiating
        # API connectors, etc.) raise non-ModuleNotFound exceptions
        # before the StateGraph is in scope. Dogfood: langchain-academy
        # module-1 emits OpenAIError("Missing credentials") at import
        # because `ChatOpenAI()` runs at module scope. AST fallback
        # ignores all of that — it walks the syntax tree without
        # executing the module — so route through it before giving up.
        ast_graph = _try_ast_extract(path=path, source_path=path)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise
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
    except Exception:  # noqa: BLE001 — symmetry with parse_file
        ast_graph = _try_ast_extract(content=content, source_path=filename)
        if ast_graph is not None and ast_graph["nodes"]:
            return ast_graph
        raise
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
    """Walks a Python AST collecting StateGraph builder calls.

    Multi-graph aware (dogfood: open_deep_research/legacy/graph.py
    constructed two independent StateGraphs in one module — a subgraph
    `section_builder` and the main graph `builder`. The previous
    flat-merge implementation produced six false-positive
    `unreachable_node` warnings because nodes from `builder` got merged
    into `section_builder`'s reachability computation under the latter's
    entry point.)

    Each `<var> = StateGraph(...)` assignment owns its own
    nodes/edges/entry state, and all subsequent `<var>.add_node(...)`
    calls are routed to that graph. The `build()` method picks **one**
    graph to return: prefer the last variable that was passed to
    `<var>.compile()`, otherwise fall back to the largest graph.
    """

    # Constructor names that produce a StateGraph-like instance.
    _GRAPH_CTORS = ("StateGraph", "MessageGraph", "Graph")
    # Sentinels used by langgraph for entry/exit; never become real nodes.
    _SENTINELS = ("START", "END", "__start__", "__end__")
    # Built-in router functions imported from `langgraph.prebuilt` etc.
    # Their `-> Literal[...]` annotation lives in the library, not in
    # the user's source, so the AST visitor can't see it via
    # visit_FunctionDef. We hard-code the destination set so that
    # `add_conditional_edges("agent", tools_condition)` correctly
    # surfaces the {tools, END} branches. Dogfood: langchain-academy
    # module-1 / module-4 — every tool-calling agent in the official
    # course produced a `tools` `unreachable_node` warning before this.
    _BUILTIN_ROUTER_LITERALS: Dict[str, set[str]] = {
        # tools_condition returns Literal["tools", "__end__"]
        "tools_condition": {"tools", "__end__"},
    }

    def __init__(self, source_path: str) -> None:
        self._source_path = source_path
        # var name → graph_id (graph_id IS the var name; class kept this
        # explicit so future work can disambiguate `self.builder` vs
        # global `builder`).
        self._builder_vars: Dict[str, str] = {}
        # graph_id → {"nodes": {...}, "edges": [...], "entry": "..."}
        self._graphs: Dict[str, Dict[str, Any]] = {}
        # Variables that received a `<var>.compile()` result. The last
        # one wins as the "main" graph (matches the LangGraph idiom
        # `graph = builder.compile()` at the bottom of a module).
        self._compile_order: list[str] = []
        # Per-call unique counter so multiple `add_node(<runtime_var>, …)`
        # calls in the same file don't all collapse to a single
        # `<dynamic>` ID — that produced false-positive cycle_detection
        # findings on langchain's own factory.py (dogfood) where the
        # collapsed self-loop wasn't real.
        self._dyn_counter: int = 0
        # function name → set of node names it dispatches to via
        # `Command(goto=...)`. Built by visit_FunctionDef before the
        # visitor reaches `<builder>.add_node(...)` so that when
        # `add_node("human_feedback", human_feedback)` resolves, we
        # can synthesise the implicit edges that LangGraph derives at
        # runtime from `def human_feedback(...) -> Command[Literal[...]]`
        # type annotations.
        #
        # Dogfood (open_deep_research/legacy/graph.py): without this
        # extraction the "human_feedback" node looked like a dead-end
        # in the static graph because its outgoing edges were dynamic
        # (return-value driven), producing 4 false-positive
        # `unreachable_node` warnings on every node it could reach.
        # Pre-populated with builtin router literal annotations
        # (tools_condition etc) — see _BUILTIN_ROUTER_LITERALS.
        self._command_goto_map: Dict[str, set[str]] = {
            name: set(dests) for name, dests in self._BUILTIN_ROUTER_LITERALS.items()
        }

    # ---- Command(goto=...) discovery -----------------------------------

    def visit_FunctionDef(self, node: _ast.FunctionDef) -> None:
        self._collect_command_goto(node)
        self.generic_visit(node)

    def visit_AsyncFunctionDef(self, node: _ast.AsyncFunctionDef) -> None:  # type: ignore[override]
        self._collect_command_goto(node)
        self.generic_visit(node)

    def _collect_command_goto(self, fn_node: Any) -> None:
        """Populate _command_goto_map[fn_node.name] = {dest1, dest2, …}.

        Source 1 (preferred — when annotated):
          def fn(...) -> Command[Literal["a", "b"]]:
        We walk the return-type subscript and pull every string Literal
        argument inside the parameterised `Literal[...]`.

        Source 2 (heuristic — when unannotated / `Command` only):
          return Command(goto="a")
          return Command(goto=["a", "b"])
        We scan the function body for `return Command(goto=...)` calls
        and harvest string literals from the goto kwarg.

        Together these handle the two patterns LangGraph examples and
        production code use: typed Command for static-analysis-friendly
        helpers (open_deep_research), and bare `Command(goto=...)` for
        ad-hoc routing logic.
        """
        dests: set[str] = set()

        # Source 1: return-type annotation Command[Literal[...]].
        ret = getattr(fn_node, "returns", None)
        if ret is not None:
            dests.update(self._extract_command_dests_from_annotation(ret))

        # Source 2: scan body for `return Command(goto=...)`.
        for sub in _ast.walk(fn_node):
            if not isinstance(sub, _ast.Return):
                continue
            val = sub.value
            if val is None:
                continue
            dests.update(self._extract_command_dests_from_return(val))

        # Sentinels (END / __end__) ARE kept in the map — downstream
        # consumers (router_literal lookup, Command(goto=END) edges)
        # need to see them so they can call `_mark_end_branch` on the
        # source. They are filtered out only at edge-materialisation
        # time inside `_handle_add_conditional` and
        # `_materialize_command_goto_edges`.
        if dests:
            existing = self._command_goto_map.setdefault(fn_node.name, set())
            existing.update(dests)

    def _extract_command_dests_from_annotation(self, ann: _ast.expr) -> set[str]:
        """Pull `Literal["a","b"]` destinations from `Command[Literal[...]]`.

        Also handles `Optional[Command[Literal[...]]]`,
        `Union[Command[Literal[...]], None]`, the older
        `Command[Literal["a"], Literal["b"]]` shape, and the
        sentinel-mixed `Literal[END, "back"]` form (where END is the
        imported identifier `from langgraph.graph import END`). Bare
        Names matching SENTINELS are surfaced verbatim so the caller
        can route them through `_mark_end_branch` rather than mistaking
        them for a real destination.
        """
        out: set[str] = set()
        elts: list[Any] = []
        for sub in _ast.walk(ann):
            if not isinstance(sub, _ast.Subscript):
                continue
            base = sub.value
            base_name = ""
            if isinstance(base, _ast.Name):
                base_name = base.id
            elif isinstance(base, _ast.Attribute):
                base_name = base.attr
            if base_name != "Literal":
                continue
            slice_node = sub.slice
            if isinstance(slice_node, _ast.Index):  # py3.8 (deprecated)
                slice_node = slice_node.value  # type: ignore[attr-defined]
            if isinstance(slice_node, _ast.Tuple):
                elts.extend(slice_node.elts)
            else:
                elts.append(slice_node)
        for elt in elts:
            if isinstance(elt, _ast.Constant) and isinstance(elt.value, str):
                out.add(elt.value)
            elif isinstance(elt, _ast.Name) and elt.id in self._SENTINELS:
                out.add(elt.id)
            elif isinstance(elt, _ast.Attribute) and elt.attr in self._SENTINELS:
                out.add(elt.attr)
        return out

    def _extract_command_dests_from_return(self, val: _ast.expr) -> set[str]:
        """Harvest `Command(goto="x")` / `Command(goto=["x","y"])` strings.

        Only string literals are collected — non-literal `goto=cond`
        expressions are skipped (we can't statically know the value).
        """
        out: set[str] = set()
        if not isinstance(val, _ast.Call):
            return out
        callee = val.func
        callee_name = ""
        if isinstance(callee, _ast.Name):
            callee_name = callee.id
        elif isinstance(callee, _ast.Attribute):
            callee_name = callee.attr
        if callee_name != "Command":
            return out
        for kw in val.keywords:
            if kw.arg != "goto":
                continue
            v = kw.value
            if isinstance(v, _ast.Constant) and isinstance(v.value, str):
                out.add(v.value)
            elif isinstance(v, (_ast.List, _ast.Tuple)):
                for elt in v.elts:
                    if isinstance(elt, _ast.Constant) and isinstance(elt.value, str):
                        out.add(elt.value)
        return out

    # ---- builder discovery ---------------------------------------------

    def visit_Assign(self, node: _ast.Assign) -> None:
        # Detect `<name> = StateGraph(...)`.
        if (
            isinstance(node.value, _ast.Call)
            and self._call_name(node.value) in self._GRAPH_CTORS
        ):
            for target in node.targets:
                self._register_builder_target(target)
        # Detect `<name> = <existing_builder>.compile()` — used to mark
        # which graph is the "main" one when multiple StateGraphs live
        # in the same file (e.g. open_deep_research subgraph
        # composition).
        if isinstance(node.value, _ast.Call):
            compiled_var = self._compile_target(node.value)
            if compiled_var is not None:
                self._compile_order.append(compiled_var)
        self.generic_visit(node)

    def visit_AnnAssign(self, node: _ast.AnnAssign) -> None:
        if (
            node.value is not None
            and isinstance(node.value, _ast.Call)
            and self._call_name(node.value) in self._GRAPH_CTORS
        ):
            self._register_builder_target(node.target)
        if node.value is not None and isinstance(node.value, _ast.Call):
            compiled_var = self._compile_target(node.value)
            if compiled_var is not None:
                self._compile_order.append(compiled_var)
        self.generic_visit(node)

    def _register_builder_target(self, target: _ast.expr) -> None:
        if isinstance(target, _ast.Name):
            self._builder_vars[target.id] = target.id
            self._ensure_graph(target.id)
        elif isinstance(target, _ast.Attribute):
            # `self.builder = StateGraph(...)` — graph_id = attr name.
            self._builder_vars[target.attr] = target.attr
            self._ensure_graph(target.attr)

    def _compile_target(self, call: _ast.Call) -> Optional[str]:
        """If `call` is `<builder_var>.compile(...)`, return the var name."""
        if not isinstance(call.func, _ast.Attribute):
            return None
        if call.func.attr != "compile":
            return None
        receiver = call.func.value
        if isinstance(receiver, _ast.Name) and receiver.id in self._builder_vars:
            return receiver.id
        if isinstance(receiver, _ast.Attribute) and receiver.attr in self._builder_vars:
            return receiver.attr
        return None

    def _ensure_graph(self, graph_id: str) -> None:
        if graph_id not in self._graphs:
            self._graphs[graph_id] = {"nodes": {}, "edges": [], "entry": ""}

    # ---- builder method dispatch ---------------------------------------

    def visit_Call(self, node: _ast.Call) -> None:
        if isinstance(node.func, _ast.Attribute):
            method = node.func.attr
            graph_id = self._builder_graph_id(node.func.value)
            if graph_id is not None:
                if method == "add_node":
                    self._handle_add_node(graph_id, node)
                elif method == "add_edge":
                    self._handle_add_edge(graph_id, node)
                elif method == "add_conditional_edges":
                    self._handle_add_conditional(graph_id, node)
                elif method == "set_entry_point":
                    self._handle_set_entry(graph_id, node)
        self.generic_visit(node)

    def _builder_graph_id(self, expr: _ast.expr) -> Optional[str]:
        """Return graph_id when `expr` references a StateGraph builder var.

        Handles `builder.add_node(...)`, `self.workflow.add_node(...)`,
        `self.builder.compile().add_node(...)` (we recurse and look up
        the underlying name).
        """
        if isinstance(expr, _ast.Name):
            return self._builder_vars.get(expr.id)
        if isinstance(expr, _ast.Attribute):
            return self._builder_vars.get(expr.attr)
        if isinstance(expr, _ast.Call):
            # Chained: `builder.compile().add_node(...)` — recurse.
            if isinstance(expr.func, (_ast.Attribute, _ast.Name)):
                return self._builder_graph_id(expr.func)
        return None

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

    def _ensure_node(self, graph_id: str, name: str, ntype: str = "llm") -> None:
        nodes = self._graphs[graph_id]["nodes"]
        if name in nodes:
            return
        nodes[name] = {
            "id":     name,
            "name":   name,
            "type":   ntype,
            "config": {},
            "pos":    {"file": self._source_path, "line": 0, "col": 0},
        }

    def _mark_end_branch(self, graph_id: str, src: str) -> None:
        """Flag `src` as having a static edge to LangGraph's END sentinel.

        Sentinels (`END` / `__end__`) are not materialised as graph
        nodes (they would falsely satisfy `loop_guard` etc), so any
        source-of-truth for "this node has an exit branch" must live
        on the source node itself. The cycle_detection rule reads
        `Node.HasExitBranch` to recognise bounded cycles whose only
        exit is via END — without this, conditionals that route to
        `Literal[END, "back_to_loop"]` (the most common LangGraph
        human-in-the-loop / reflection idiom) get classified Critical
        despite a structural exit. Dogfood: company-researcher's
        `route_from_reflection`.

        We emit BOTH the typed top-level `has_exit_branch` field
        (consumed by the Go domain layer) and a `config.has_end_branch`
        legacy key for ANY downstream tool that may already key on it.
        Domain rules read the typed field only.
        """
        node_obj = self._graphs[graph_id]["nodes"].get(src)
        if node_obj is None:
            return
        node_obj["has_exit_branch"] = True
        cfg = node_obj.setdefault("config", {})
        cfg["has_end_branch"] = True

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

    def _handle_add_node(self, graph_id: str, node: _ast.Call) -> None:
        if not node.args:
            return
        # add_node("name", fn) OR add_node(fn) (1-arg sugar where fn.__name__
        # is the name in newer langgraph). Non-literal name args get a
        # PER-CALL unique placeholder so we don't collapse N distinct
        # `add_node(state.var, fn_n)` calls onto one `<dynamic>` node and
        # invent self-cycles between them.
        if len(node.args) >= 2:
            name = self._str_arg(node.args[0]) or self._next_dynamic_name()
            fn = node.args[1]
            self._ensure_node(graph_id, name, self._node_type_for(fn))
            self._record_handler(graph_id, name, fn)
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
            self._ensure_node(graph_id, name, self._node_type_for(fn))
            self._record_handler(graph_id, name, fn)

    def _record_handler(self, graph_id: str, node_name: str, fn: _ast.expr) -> None:
        """Stash the (node_name → handler-function-name) link.

        Used by `build()` to materialise Command(goto=...) implicit
        edges. Only string-resolvable handlers are recorded; lambda /
        instance-method / chained call handlers fall through.
        """
        fn_name = ""
        if isinstance(fn, _ast.Name):
            fn_name = fn.id
        elif isinstance(fn, _ast.Attribute):
            fn_name = fn.attr
        if not fn_name:
            return
        graph = self._graphs[graph_id]
        handlers = graph.setdefault("handlers", {})
        handlers[node_name] = fn_name

    def _next_dynamic_name(self) -> str:
        self._dyn_counter += 1
        return f"<dynamic_{self._dyn_counter}>"

    def _handle_add_edge(self, graph_id: str, node: _ast.Call) -> None:
        if len(node.args) < 2:
            return
        src_arg, dst_arg = node.args[0], node.args[1]
        # LangGraph fan-in form: `add_edge(["a", "b"], "c")` produces
        # edges {a → c, b → c}. Dogfood (langgraph/libs/langgraph/bench/
        # wide_state.py): without list-src support `five` and `six`
        # showed up as `unreachable_node` because the join node was
        # only reachable via the list form.
        srcs: list[str] = []
        if isinstance(src_arg, (_ast.List, _ast.Tuple)):
            for elt in src_arg.elts:
                v = self._sentinel_arg(elt) or self._str_arg(elt)
                if v is not None:
                    srcs.append(v)
        else:
            v = self._sentinel_arg(src_arg) or self._str_arg(src_arg)
            if v is not None:
                srcs.append(v)
        dst = self._sentinel_arg(dst_arg) or self._str_arg(dst_arg)
        # Skip edges whose endpoints aren't string literals — we can't
        # know what they connect, and inventing a synthetic placeholder
        # creates false-positive cycles (langchain factory.py dogfood).
        if not srcs or dst is None:
            return
        graph = self._graphs[graph_id]
        for src in srcs:
            if src in self._SENTINELS:
                # START → user-node: the user-node becomes the entry. Drop the edge.
                if dst not in self._SENTINELS and not graph["entry"]:
                    graph["entry"] = dst
                    self._ensure_node(graph_id, dst, "llm")
                continue
            if dst in self._SENTINELS:
                # User-node → END: drop the edge but mark the source as
                # having an explicit END branch so cycle_detection can
                # recognise the cycle as bounded.
                self._ensure_node(graph_id, src, "llm")
                self._mark_end_branch(graph_id, src)
                continue
            self._ensure_node(graph_id, src, "llm")
            self._ensure_node(graph_id, dst, "llm")
            graph["edges"].append({"from": src, "to": dst})

    def _handle_add_conditional(self, graph_id: str, node: _ast.Call) -> None:
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
        self._ensure_node(graph_id, src, "llm")
        # Extract destination nodes from the third positional arg
        # (or `path_map`/`mapping`/`then` keyword). Two shapes:
        #   1. dict {"label": "node_name", …} — labelled mapping
        #   2. list/tuple ["node_a", "node_b"] — `path_map` shorthand
        # The list form is what open_deep_research/legacy/graph.py
        # uses on the `gather_completed_sections` conditional —
        # missing list support left two LangGraph nodes apparently
        # unreachable in the AST fallback.
        mapping_node: Optional[_ast.expr] = None
        if len(node.args) >= 3:
            cand = node.args[2]
            if isinstance(cand, (_ast.Dict, _ast.List, _ast.Tuple)):
                mapping_node = cand
        for kw in node.keywords:
            if kw.arg in ("path_map", "mapping", "then") and isinstance(
                kw.value, (_ast.Dict, _ast.List, _ast.Tuple)
            ):
                mapping_node = kw.value
                break
        if mapping_node is None:
            # No explicit mapping argument. LangGraph still resolves
            # destinations at runtime by reading the router function's
            # `-> Literal[...]` return-type annotation. Dogfood
            # (executive-ai-assistant/eaia/main/graph.py): 13
            # `unreachable_node` warnings on every node behind a
            # `add_conditional_edges("triage_input", route_after_triage)`
            # call where `route_after_triage` returns
            # `Literal["draft_response", "mark_as_read_node", "notify"]`.
            # Without this lookup the static graph kept all those
            # destinations apparently disconnected.
            if len(node.args) >= 2:
                router_fn = node.args[1]
                router_name = ""
                if isinstance(router_fn, _ast.Name):
                    router_name = router_fn.id
                elif isinstance(router_fn, _ast.Attribute):
                    router_name = router_fn.attr
                dests = self._command_goto_map.get(router_name) if router_name else None
                if dests:
                    graph = self._graphs[graph_id]
                    for dest in sorted(dests):
                        if not dest:
                            continue
                        if dest in self._SENTINELS:
                            self._mark_end_branch(graph_id, src)
                            continue
                        self._ensure_node(graph_id, dest, "llm")
                        graph["edges"].append({
                            "from":      src,
                            "to":        dest,
                            "condition": "router_literal",
                        })
            return
        graph = self._graphs[graph_id]
        if isinstance(mapping_node, _ast.Dict):
            for key, value in zip(mapping_node.keys, mapping_node.values):
                cond = self._str_arg(key) if key is not None else ""
                target = self._sentinel_arg(value) or self._str_arg(value)
                if not target:
                    continue
                if target in self._SENTINELS:
                    self._mark_end_branch(graph_id, src)
                    continue
                self._ensure_node(graph_id, target, "llm")
                edge = {"from": src, "to": target}
                if cond:
                    edge["condition"] = cond
                graph["edges"].append(edge)
        else:  # list / tuple
            for elt in mapping_node.elts:
                target = self._sentinel_arg(elt) or self._str_arg(elt)
                if not target:
                    continue
                if target in self._SENTINELS:
                    self._mark_end_branch(graph_id, src)
                    continue
                self._ensure_node(graph_id, target, "llm")
                graph["edges"].append({"from": src, "to": target})

    def _handle_set_entry(self, graph_id: str, node: _ast.Call) -> None:
        if not node.args:
            return
        name = self._str_arg(node.args[0])
        graph = self._graphs[graph_id]
        if name and not graph["entry"]:
            graph["entry"] = name
            self._ensure_node(graph_id, name, "llm")

    def _materialize_command_goto_edges(self, graph: Dict[str, Any]) -> int:
        """For each node whose handler returns Command(goto="<dest>"),
        synthesise the implicit edges LangGraph would derive at runtime.

        Skips destinations not present in this graph's nodes (the
        Command return type may name nodes from a different subgraph,
        in which case the edge would be misleading). Skips edges
        already present so a partial conditional mapping doesn't get
        duplicated.

        Returns the number of edges synthesised — surfaced in metadata
        so the consumer can tell that some over-approximation
        happened.
        """
        handlers = graph.get("handlers") or {}
        existing = {(e["from"], e["to"]) for e in graph["edges"]}
        synthesised = 0
        for node_name, fn_name in handlers.items():
            dests = self._command_goto_map.get(fn_name)
            if not dests:
                continue
            for dest in sorted(dests):
                # Sentinel destination (END / __end__) → mark exit on
                # the source rather than synthesising a fake edge.
                if dest in self._SENTINELS:
                    if node_name in graph["nodes"]:
                        n = graph["nodes"][node_name]
                        n["has_exit_branch"] = True
                        n.setdefault("config", {})["has_end_branch"] = True
                    continue
                if dest not in graph["nodes"]:
                    continue
                if (node_name, dest) in existing:
                    continue
                graph["edges"].append({
                    "from": node_name,
                    "to": dest,
                    "condition": "command_goto",
                })
                existing.add((node_name, dest))
                synthesised += 1
        return synthesised

    def build(self) -> Dict[str, Any]:
        # Pick the "main" graph among potentially many StateGraphs in the
        # same module: prefer the last one passed to `<var>.compile()`
        # (idiomatic `graph = builder.compile()` at module bottom).
        # Otherwise fall back to the largest graph by node count.
        chosen_id = ""
        for cid in reversed(self._compile_order):
            if cid in self._graphs:
                chosen_id = cid
                break
        if not chosen_id and self._graphs:
            chosen_id = max(
                self._graphs.keys(),
                key=lambda gid: len(self._graphs[gid]["nodes"]),
            )
        if not chosen_id:
            return {
                "nodes": [],
                "edges": [],
                "entry_node_id": "",
                "metadata": {
                    "source_format":           "langgraph",
                    "source_file":             self._source_path,
                    "extraction":              "ast_fallback",
                    "conditional_edge_reason": "over_approximated_dynamic",
                },
            }
        graph = self._graphs[chosen_id]
        synthesised = self._materialize_command_goto_edges(graph)
        out_nodes = list(graph["nodes"].values())
        entry = graph["entry"]
        if not entry and out_nodes:
            entry = out_nodes[0]["id"]
        meta = {
            "source_format":           "langgraph",
            "source_file":             self._source_path,
            "extraction":              "ast_fallback",
            "conditional_edge_reason": "over_approximated_dynamic",
            "selected_graph":          chosen_id,
            "discovered_graph_count":  len(self._graphs),
            "command_goto_edges":      synthesised,
        }
        return {
            "nodes": out_nodes,
            "edges": graph["edges"],
            "entry_node_id": entry,
            "metadata": meta,
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
