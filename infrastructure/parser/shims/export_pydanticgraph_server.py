#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Long-lived JSON-RPC worker that exports pydantic-graph workflow definitions
into Shingan's WorkflowGraph JSON format.

Strategy: AST-only (ADR-015 PoC, mirroring the LangGraph.js shim)
=================================================================
This shim NEVER imports ``pydantic_graph``. It parses the user's Python source
with the stdlib ``ast`` module and derives the workflow graph from the static
structure alone:

  * Nodes are classes subclassing ``BaseNode`` (e.g.
    ``class Foo(BaseNode): async def run(self, ctx) -> Bar | Baz: ...``).
  * Edges come from each node's ``run`` method RETURN-TYPE ANNOTATION: the
    union of next ``BaseNode`` subclasses. ``run() -> Bar | Baz`` ⇒ edges
    Foo→Bar, Foo→Baz.
  * ``End`` / ``End[...]`` in the union ⇒ the node is an exit
    (``has_exit_branch=true``); End is a sentinel, never a node, never an edge.
  * ``Self`` in the union ⇒ a self-edge back to the containing class (the
    canonical "loop until End" pydantic-graph idiom).

Because the analysis is purely static, there is no "framework not installed"
gate — Python 3 alone is sufficient, exactly like the AST-only Node shim.

Protocol
========
Newline-delimited JSON (one request per line on stdin, one response per line
on stdout). All log output and tracebacks go to stderr — stdout MUST contain
only response frames.

Methods
-------
- ``health_check``   → ``{"status": "ok", "pydantic_graph_version": "ast"}``
                      (always "ok" when Python loads; never imports the lib)
- ``parse_file``     → ``{"path": <abs path>}`` → WorkflowGraph JSON
- ``parse_content``  → ``{"content": <python source>, "filename": <hint>}``
                      → WorkflowGraph JSON
- ``shutdown``       → ``{"ok": true}`` then process exits.

Robust: syntax errors / non-pydantic files → empty graph, never crash the
worker.

ADR references
--------------
- ADR-015: framework-by-framework PoC parsers (pydantic-graph after LangGraph.js).
- ADR-014: AST-primary extraction to sidestep the runtime-introspection trap.
- ADR-009: long-lived worker (no per-file fork) + degraded mode.
"""
from __future__ import annotations

import ast as _ast
import json
import sys
import traceback
from typing import Any, Dict, List, Optional, Set, Tuple

# ----- stdout/stderr discipline ---------------------------------------------
# Keep stdout reserved for response frames; route any stray print() to stderr.
_RESPONSE_STREAM = sys.stdout
sys.stdout = sys.stderr


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


# ----- AST helpers -----------------------------------------------------------
# The "End" sentinel name (pydantic_graph.End) and "Self" (typing.Self). Both
# are name-based: we never import the framework, so we recognise them by the
# identifier the user wrote.
_END_NAMES = {"End"}
_SELF_NAMES = {"Self"}
# Union-constructor names whose subscript elements are themselves type leaves.
_UNION_NAMES = {"Union", "Optional"}


def _base_name(base: _ast.expr) -> Optional[str]:
    """Return the bare identifier of a class base, looking through subscripts
    and attribute access so all of ``BaseNode``, ``BaseNode[State]``,
    ``BaseNode[State, Deps, int]`` and ``pkg.BaseNode`` resolve to "BaseNode".
    """
    if isinstance(base, _ast.Name):
        return base.id
    if isinstance(base, _ast.Attribute):
        return base.attr
    if isinstance(base, _ast.Subscript):
        return _base_name(base.value)
    return None


def _is_base_node_class(node: _ast.ClassDef) -> bool:
    """True when the class subclasses ``BaseNode`` (directly, by any of the
    spellings _base_name understands). We deliberately match on the leaf name
    "BaseNode" rather than resolving imports — the framework is never loaded.
    """
    for base in node.bases:
        if _base_name(base) == "BaseNode":
            return True
    return False


def _leaf_name(expr: _ast.expr) -> Optional[str]:
    """Return the identifier of a single type leaf:
        Bar          → "Bar"        (Name)
        End[int]     → "End"        (Subscript of Name)
        mod.Bar      → "Bar"        (Attribute)
        'Bar'        → "Bar"        (string forward-ref Constant)
    None for anything we don't recognise (complex generics, literals, etc.).
    """
    if isinstance(expr, _ast.Name):
        return expr.id
    if isinstance(expr, _ast.Attribute):
        return expr.attr
    if isinstance(expr, _ast.Subscript):
        return _leaf_name(expr.value)
    if isinstance(expr, _ast.Constant) and isinstance(expr.value, str):
        # Forward reference, e.g. `-> 'Bar'`. We resolve it against the known
        # node set later; an unparseable/qualified string degrades to None.
        ref = expr.value.strip()
        # Strip a trailing subscript in the string form (`'End[int]'`).
        head = ref.split("[", 1)[0].strip()
        # Take the last dotted segment (`'pkg.Bar'` → 'Bar').
        head = head.rsplit(".", 1)[-1].strip()
        return head or None
    return None


def _flatten_union(expr: Optional[_ast.expr]) -> List[_ast.expr]:
    """Flatten a return annotation into its individual type leaves.

    Handles:
      * ``A | B``           (PEP 604 BinOp/BitOr; ``A | B | C`` nests left)
      * ``Union[A, B]`` / ``Optional[A]``  (Subscript of a Union/Optional name)
      * bare ``A``          (Name / Attribute / Subscript / forward-ref string)
    Anything else is returned as a single opaque leaf so the caller can try
    ``_leaf_name`` on it (and drop it if unrecognised).
    """
    if expr is None:
        return []
    # Whole-annotation forward-ref string, e.g. `-> "Bar | Baz"` or
    # `-> "Union[Bar, Baz]"`. Re-parse the string as an expression so the union
    # flattens like a real annotation; on failure fall through and let
    # _leaf_name try the raw string. (PEP 563 / `from __future__ import
    # annotations` makes string-form unions common.)
    if isinstance(expr, _ast.Constant) and isinstance(expr.value, str):
        try:
            inner = _ast.parse(expr.value.strip(), mode="eval").body
        except (SyntaxError, ValueError):
            return [expr]
        return _flatten_union(inner)
    # PEP 604 union: A | B  → BinOp(op=BitOr).
    if isinstance(expr, _ast.BinOp) and isinstance(expr.op, _ast.BitOr):
        return _flatten_union(expr.left) + _flatten_union(expr.right)
    # Union[...] / Optional[...].
    if isinstance(expr, _ast.Subscript):
        head = _base_name(expr.value)
        if head in _UNION_NAMES:
            sl = expr.slice
            # py3.9+: slice is the expression directly; a Tuple for multi-arg.
            if isinstance(sl, _ast.Tuple):
                leaves: List[_ast.expr] = []
                for e in sl.elts:
                    leaves.extend(_flatten_union(e))
                return leaves
            return _flatten_union(sl)
        # Non-union subscript (e.g. End[int], List[Foo]) is a single leaf.
        return [expr]
    return [expr]


def _safe_id(name: str) -> str:
    out: List[str] = []
    for ch in str(name).strip():
        if ch.isalnum() or ch in "_-./":
            out.append(ch)
        elif ch.isspace():
            out.append("_")
    return "".join(out).strip("_") or "node"


def _find_run_method(cls: _ast.ClassDef) -> Optional[_ast.expr]:
    """Return the return-annotation expr of the class's ``run`` method
    (sync or async), or None if there is no ``run`` / no annotation.
    """
    for item in cls.body:
        if isinstance(item, (_ast.FunctionDef, _ast.AsyncFunctionDef)) and item.name == "run":
            return item.returns
    return None


# ----- graph builder ---------------------------------------------------------
class _PydanticGraphASTVisitor:
    """Two-pass extractor over a parsed module.

    Pass 1 collects every BaseNode subclass (name → ClassDef + position).
    Pass 2 reads each node's ``run`` return annotation, flattens the union and
    emits an edge for every leaf that names a *known* node. ``End`` sets
    has_exit_branch; ``Self`` emits a self-edge. Unknown leaves (third-party
    types, unresolved forward refs, complex generics) are dropped — we omit
    edges rather than invent them (the spec's degrade-gracefully contract).
    """

    def __init__(self, source_path: str) -> None:
        self._source_path = source_path
        # name → (ClassDef, lineno, col)
        self._classes: Dict[str, _ast.ClassDef] = {}
        # Preserve declaration order for entry-point fallback ("first registered").
        self._order: List[str] = []
        # Optional explicit start node names harvested from `Graph(...).run(Start())`
        # / a `start_node=` kwarg, when present.
        self._explicit_starts: List[str] = []
        # Node names registered via Graph(nodes=[...]); used to scope the graph
        # when present (a module may define helper BaseNode classes that aren't
        # part of the Graph). Empty ⇒ fall back to all discovered classes.
        self._registered: List[str] = []

    # ---- Pass 1: collect classes -------------------------------------------
    def collect(self, tree: _ast.Module) -> None:
        for node in _ast.walk(tree):
            if isinstance(node, _ast.ClassDef) and _is_base_node_class(node):
                if node.name not in self._classes:
                    self._classes[node.name] = node
                    self._order.append(node.name)
            elif isinstance(node, _ast.Call):
                self._maybe_graph_call(node)

    def _maybe_graph_call(self, call: _ast.Call) -> None:
        # Graph(nodes=[Foo, Bar, ...]) → register the node set.
        func = call.func
        fname = None
        if isinstance(func, _ast.Name):
            fname = func.id
        elif isinstance(func, _ast.Attribute):
            fname = func.attr
        if fname == "Graph":
            for kw in call.keywords:
                if kw.arg == "nodes":
                    for el in self._iter_seq(kw.value):
                        nm = self._node_ref_name(el)
                        if nm:
                            self._registered.append(nm)
            # Positional nodes=[...] (first positional arg) too.
            if call.args:
                for el in self._iter_seq(call.args[0]):
                    nm = self._node_ref_name(el)
                    if nm:
                        self._registered.append(nm)
            for kw in call.keywords:
                if kw.arg == "start_node":
                    nm = self._node_ref_name(kw.value)
                    if nm:
                        self._explicit_starts.append(nm)
        # graph.run(StartNode(...)) / graph.run_sync(StartNode(...)).
        elif fname in ("run", "run_sync", "iter"):
            if call.args:
                nm = self._node_ref_name(call.args[0])
                if nm:
                    self._explicit_starts.append(nm)

    @staticmethod
    def _iter_seq(expr: _ast.expr):
        if isinstance(expr, (_ast.List, _ast.Tuple, _ast.Set)):
            return list(expr.elts)
        return []

    @staticmethod
    def _node_ref_name(expr: _ast.expr) -> Optional[str]:
        # Foo            → "Foo"        (class reference in nodes=[...])
        # Foo()          → "Foo"        (instance as start node)
        # mod.Foo / .Foo() → "Foo"
        if isinstance(expr, _ast.Name):
            return expr.id
        if isinstance(expr, _ast.Attribute):
            return expr.attr
        if isinstance(expr, _ast.Call):
            return _PydanticGraphASTVisitor._node_ref_name(expr.func)
        return None

    # ---- Pass 2: build graph -----------------------------------------------
    def build(self) -> Dict[str, Any]:
        known: Set[str] = set(self._classes.keys())
        # Scope to the registered node set when a Graph(...) named one and at
        # least one of its members is a class we discovered; otherwise use all
        # discovered BaseNode classes. This keeps helper/abstract base classes
        # out of the graph without dropping nodes when the registration list
        # references names we couldn't resolve.
        scoped = [n for n in self._registered if n in known]
        if scoped:
            node_names = [n for n in self._order if n in scoped]
        else:
            node_names = list(self._order)

        out_nodes: List[Dict[str, Any]] = []
        out_edges: List[Dict[str, Any]] = []
        exit_nodes: Set[str] = set()
        # Count non-self in-edges per node so a loop-only node (its sole in-edge
        # being its own self-loop) still qualifies as a zero-in-degree entry.
        real_indeg: Dict[str, int] = {n: 0 for n in node_names}

        node_set = set(node_names)
        for name in node_names:
            cls = self._classes[name]
            returns = _find_run_method(cls)
            has_exit = False
            seen_targets: Set[str] = set()
            for leaf in _flatten_union(returns):
                lname = _leaf_name(leaf)
                if lname is None:
                    continue
                if lname in _END_NAMES:
                    has_exit = True
                    continue
                if lname in _SELF_NAMES:
                    # Self-edge: the canonical "loop until End" idiom. This is
                    # load-bearing for the cycle-with-End acceptance fixture.
                    # Self-loops do NOT bump real_indeg — see entry inference.
                    if name not in seen_targets:
                        seen_targets.add(name)
                        out_edges.append({"from": name, "to": name})
                    continue
                if lname in node_set and lname not in seen_targets:
                    seen_targets.add(lname)
                    out_edges.append({"from": name, "to": lname})
                    if lname != name:
                        real_indeg[lname] = real_indeg.get(lname, 0) + 1
                # else: unknown leaf (End-less sentinel, third-party type,
                # unresolved forward-ref) — omit the edge, never invent a node.
            if has_exit:
                exit_nodes.add(name)

        for name in node_names:
            cls = self._classes[name]
            out_nodes.append({
                "id":   name,
                "name": name,
                # A pydantic-graph node is a leaf unit of agent work whose body
                # we don't introspect → NodeTypeTask (parity with the "step"
                # framing in domain/graph.go). Non-loop, so the self-loop in the
                # acceptance fixture correctly downgrades to Warning, and the
                # tool-oriented rules don't false-positive on arbitrary bodies.
                "type": "task",
                "config": {},
                "pos": {
                    "file": self._source_path,
                    "line": getattr(cls, "lineno", 0),
                    "col":  getattr(cls, "col_offset", 0),
                },
                "has_exit_branch": name in exit_nodes,
            })

        entry, ambiguous = self._infer_entry(node_names, real_indeg)

        metadata: Dict[str, Any] = {
            "source_format": "pydantic-graph",
            "source_file":   self._source_path,
            "pydantic_graph_version": "ast",
            "extraction":    "ast",
            "conditional_edge_reason": "exact_static_match",
        }
        if ambiguous:
            metadata["entry_ambiguous"] = True

        return {
            "nodes": out_nodes,
            "edges": out_edges,
            "entry_node_id": entry,
            "metadata": metadata,
        }

    def _infer_entry(self, node_names: List[str], real_indeg: Dict[str, int]) -> Tuple[str, bool]:
        if not node_names:
            return "", False
        # 1. Explicit start (graph.run(Start()) / start_node=) wins.
        for s in self._explicit_starts:
            if s in node_names:
                return s, False
        # 2. The node that is never a (non-self) destination. real_indeg already
        #    excludes self-loops, so a loop-only node still counts as a root
        #    (the react-loop fixture: `A` with run() -> A | End is the entry).
        zero_in = [n for n in node_names if real_indeg.get(n, 0) == 0]
        if len(zero_in) == 1:
            return zero_in[0], False
        if len(zero_in) > 1:
            # Multiple roots — pick the first registered for determinism but
            # flag the ambiguity in metadata.
            return zero_in[0], True
        # 3. No zero in-degree node (fully cyclic) → first registered, ambiguous.
        return node_names[0], True


def _try_extract(*, path: Optional[str] = None, content: Optional[str] = None,
                 source_path: str) -> Dict[str, Any]:
    """Parse the source and return a WorkflowGraph dict. On syntax error /
    unreadable file, returns an empty graph (never raises) so the worker stays
    alive and the directory walk continues.
    """
    try:
        if content is None:
            with open(path, encoding="utf-8") as fp:
                content = fp.read()
        tree = _ast.parse(content, filename=source_path)
    except (OSError, SyntaxError, UnicodeDecodeError, ValueError):
        return _empty_graph(source_path)
    visitor = _PydanticGraphASTVisitor(source_path)
    visitor.collect(tree)
    return visitor.build()


def _empty_graph(source_path: str) -> Dict[str, Any]:
    return {
        "nodes": [],
        "edges": [],
        "entry_node_id": "",
        "metadata": {
            "source_format": "pydantic-graph",
            "source_file":   source_path,
            "pydantic_graph_version": "ast",
            "extraction":    "ast_empty",
            "conditional_edge_reason": "exact_static_match",
        },
    }


# ----- handlers --------------------------------------------------------------
def _handle_health_check(_: Dict[str, Any]) -> Dict[str, Any]:
    # AST-only: we never import pydantic_graph, so health is "ok" whenever
    # Python itself loaded this shim. The version string is the sentinel "ast"
    # to make the strategy explicit in diagnostics.
    return {
        "status": "ok",
        "pydantic_graph_version": "ast",
        "python": sys.version,
    }


def _handle_parse_file(params: Dict[str, Any]) -> Dict[str, Any]:
    path = params.get("path")
    if not path:
        raise ValueError("parse_file: 'path' is required")
    return _try_extract(path=path, source_path=path)


def _handle_parse_content(params: Dict[str, Any]) -> Dict[str, Any]:
    content = params.get("content")
    if content is None:
        raise ValueError("parse_content: 'content' is required")
    filename = params.get("filename") or "<inline.py>"
    return _try_extract(content=content, source_path=filename)


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
