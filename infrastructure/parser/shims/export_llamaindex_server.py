#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Long-lived JSON-RPC worker that exports LlamaIndex Workflows definitions
into Shingan's WorkflowGraph JSON format.

Strategy: AST-only (ADR-015 PoC, mirroring the pydantic-graph + LangGraph.js
shims)
=======================================================================
This shim NEVER imports ``llama_index``. It parses the user's Python source
with the stdlib ``ast`` module and derives the workflow graph from the static
structure alone.

LlamaIndex Workflows are EVENT-DRIVEN, so the graph shape is derived very
differently from pydantic-graph (where the return-type leaf *is* the target
node). Here events are an indirection layer; STEPS are the nodes and EDGES are
inferred by matching event types between steps:

  * A workflow is a class subclassing ``Workflow``. Its steps are methods
    decorated ``@step`` (or ``@step(...)``, ``@x.step`` — matched by leaf name).
  * Each ``@step`` method CONSUMES an event (the type annotation of its
    non-``self``, non-``Context`` parameter) and PRODUCES an event (its
    return-type annotation, which may be a union for branching).
  * Edges come from matching event types across steps: build ``producers[E]``
    (steps whose return annotation includes event type ``E``) and
    ``consumers[E]`` (steps whose event param type is ``E``); for each ``E``
    emit an edge from every producer to every consumer (cross-product, deduped,
    self-loops allowed).
  * ``StartEvent`` is the entry signal: the step CONSUMING ``StartEvent`` is the
    entry node. It is a sentinel — never a node, never an edge target.
  * ``StopEvent`` is the exit sentinel: a step whose return annotation includes
    ``StopEvent`` gets ``has_exit_branch=True``. StopEvent is never a node.
  * Steps are scoped per ``Workflow`` subclass so two unrelated workflows that
    happen to share event names don't cross-link. (Multi-workflow files: only
    the FIRST workflow class is emitted — see "Honest accounting" below.)

Because the analysis is purely static, there is no "framework not installed"
gate — Python 3 alone is sufficient, exactly like the AST-only shims it mirrors.

Honest accounting (what is approximated / deferred)
---------------------------------------------------
  * Dynamic event dispatch (``ctx.send_event(...)``, ``ev.get(...)``,
    ``Event`` constructed at runtime) is NOT followed — only statically
    annotated produce/consume relationships yield edges. Unresolvable event
    types are dropped, never invented (degrade-gracefully contract).
  * Bare ``Event`` / unannotated params: a step with no resolvable event param
    type produces no in-edges; a step returning bare ``Event`` produces no
    out-edges. We omit rather than guess.
  * ``@step(num_workers=N)`` parallelism is recognised as a step but the worker
    count is not modelled (every step is one ``task`` node).
  * Multi-workflow files: only the first ``Workflow`` subclass in the module is
    exported (single-graph-per-file contract). Helper workflows are ignored.
  * Only DIRECT ``Workflow`` subclasses are detected: ``class Real(BaseFlow)``
    where ``BaseFlow(Workflow)`` is defined elsewhere yields an empty graph
    (we match the base-class leaf name without resolving the inheritance chain).
  * Node typing: every step is ``task`` (PoC). LLM classification is skipped —
    ``task`` is also what lets the cycle-with-StopEvent fixture downgrade to
    Warning.

Protocol
========
Newline-delimited JSON (one request per line on stdin, one response per line
on stdout). All log output and tracebacks go to stderr — stdout MUST contain
only response frames.

Methods
-------
- ``health_check``   → ``{"status": "ok", "llamaindex_version": "ast"}``
                      (always "ok" when Python loads; never imports the lib)
- ``parse_file``     → ``{"path": <abs path>}`` → WorkflowGraph JSON
- ``parse_content``  → ``{"content": <python source>, "filename": <hint>}``
                      → WorkflowGraph JSON
- ``shutdown``       → ``{"ok": true}`` then process exits.

Robust: syntax errors / non-workflow files → empty graph, never crash the
worker.

ADR references
--------------
- ADR-015: framework-by-framework PoC parsers (LlamaIndex Workflows is #4).
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


# ----- sentinel / framework name constants ----------------------------------
# All name-based: we never import the framework, so we recognise the framework
# types by the identifier the user wrote.
_START_EVENT_NAMES = {"StartEvent"}
_STOP_EVENT_NAMES = {"StopEvent"}
# Bare ``Event`` (the base class) carries no routable type information — every
# concrete event is an ``Event``, so matching producers/consumers on it would
# cross-link unrelated steps (and fabricate self-loops). Treat it as an opaque,
# unresolvable type: omit the edge rather than invent one.
_BASE_EVENT_NAMES = {"Event"}
# Context parameter names/types to skip when locating the consumed event param.
# LlamaIndex steps are frequently `def step(self, ctx: Context, ev: SomeEvent)`.
_CONTEXT_TYPE_NAMES = {"Context"}
_CONTEXT_PARAM_NAMES = {"ctx", "context"}
# Union-constructor names whose subscript elements are themselves type leaves.
_UNION_NAMES = {"Union", "Optional"}
# Decorator leaf name that marks a workflow step.
_STEP_DECORATOR = "step"
# Base class leaf name that marks a workflow class.
_WORKFLOW_BASE = "Workflow"


def _base_name(expr: _ast.expr) -> Optional[str]:
    """Return the bare identifier of an expression, looking through subscripts
    and attribute access so ``Workflow``, ``Workflow[State]``, ``pkg.Workflow``
    and ``deco(...)`` heads all resolve to their leaf name.
    """
    if isinstance(expr, _ast.Name):
        return expr.id
    if isinstance(expr, _ast.Attribute):
        return expr.attr
    if isinstance(expr, _ast.Subscript):
        return _base_name(expr.value)
    if isinstance(expr, _ast.Call):
        return _base_name(expr.func)
    return None


def _leaf_name(expr: _ast.expr) -> Optional[str]:
    """Return the identifier of a single type leaf:
        SomeEvent       → "SomeEvent"   (Name)
        StopEvent[int]  → "StopEvent"   (Subscript of Name)
        mod.SomeEvent   → "SomeEvent"   (Attribute)
        'SomeEvent'     → "SomeEvent"   (string forward-ref Constant)
    None for anything we don't recognise (complex generics, literals, etc.).
    """
    if isinstance(expr, _ast.Name):
        return expr.id
    if isinstance(expr, _ast.Attribute):
        return expr.attr
    if isinstance(expr, _ast.Subscript):
        return _leaf_name(expr.value)
    if isinstance(expr, _ast.Constant) and isinstance(expr.value, str):
        # Forward reference, e.g. `-> 'SomeEvent'`. Strip a trailing subscript
        # and any dotted qualifier; resolve against known events later.
        ref = expr.value.strip()
        head = ref.split("[", 1)[0].strip()
        head = head.rsplit(".", 1)[-1].strip()
        return head or None
    return None


def _flatten_union(expr: Optional[_ast.expr]) -> List[_ast.expr]:
    """Flatten a return/param annotation into its individual type leaves.

    Handles:
      * ``A | B``           (PEP 604 BinOp/BitOr; ``A | B | C`` nests left)
      * ``Union[A, B]`` / ``Optional[A]``  (Subscript of a Union/Optional name)
      * bare ``A``          (Name / Attribute / Subscript / forward-ref string)
    Anything else is returned as a single opaque leaf so the caller can try
    ``_leaf_name`` on it (and drop it if unrecognised).

    Applied to BOTH the return annotation (branching produce) AND the consumed
    event-param annotation (a step accepting ``A | B`` consumes both A and B),
    so the two sides flatten consistently.
    """
    if expr is None:
        return []
    # Whole-annotation forward-ref string, e.g. `-> "A | B"`. Re-parse so the
    # union flattens like a real annotation; on failure fall through and let
    # _leaf_name try the raw string.
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
        # Non-union subscript (e.g. StopEvent[int], List[Foo]) is a single leaf.
        return [expr]
    return [expr]


def _annotation_event_names(expr: Optional[_ast.expr]) -> List[str]:
    """Flatten an annotation to the list of distinct event-type leaf names it
    references. Drops ``None`` literals (from ``Optional``) and unresolvable
    leaves. Order-preserving, deduped.
    """
    names: List[str] = []
    for leaf in _flatten_union(expr):
        name = _leaf_name(leaf)
        if name is None or name == "None":
            continue
        if name not in names:
            names.append(name)
    return names


# ----- step decorator / class predicates ------------------------------------
def _is_step_method(func: _ast.AST) -> bool:
    """True when a (async) function is decorated with ``@step`` in any spelling:
    ``@step``, ``@step(...)``, ``@step(num_workers=N)``, ``@x.step``,
    ``@x.step(...)`` — matched by the decorator's leaf name.
    """
    if not isinstance(func, (_ast.FunctionDef, _ast.AsyncFunctionDef)):
        return False
    for deco in func.decorator_list:
        if _base_name(deco) == _STEP_DECORATOR:
            return True
    return False


def _is_workflow_class(cls: _ast.ClassDef) -> bool:
    """True when the class subclasses ``Workflow`` (directly, by any spelling
    _base_name understands). Leaf-name match — the framework is never loaded.
    """
    for base in cls.bases:
        if _base_name(base) == _WORKFLOW_BASE:
            return True
    return False


def _consumed_event(func) -> Optional[str]:
    """Return the leaf name of the single event type a step consumes, or None.

    Skips ``self``, the ``Context`` parameter (by annotation leaf ``Context``
    or by name ``ctx``/``context``), and any param without a resolvable event
    annotation. The FIRST remaining annotated param is the consumed event.

    A union param (``ev: A | B``) is treated as consuming the first leaf — but
    see ``_consumed_events`` for the full set; this single-leaf accessor is
    retained only for the common single-event case.
    """
    names = _consumed_events(func)
    return names[0] if names else None


def _consumed_events(func) -> List[str]:
    """Return the list of event leaf names a step consumes (union params fan
    out). Skips ``self`` and the ``Context`` param. The consumed event is the
    first non-self, non-Context positional param that carries a resolvable
    annotation.
    """
    args = func.args
    positional = list(args.posonlyargs) + list(args.args)
    for arg in positional:
        if arg.arg == "self":
            continue
        ann = arg.annotation
        # Skip the Context param (by annotation leaf or by conventional name).
        if ann is not None and _leaf_name(ann) in _CONTEXT_TYPE_NAMES:
            continue
        if ann is None and arg.arg in _CONTEXT_PARAM_NAMES:
            continue
        if ann is None:
            # Unannotated non-context param: no resolvable event type, keep
            # scanning in case a later param is the annotated event.
            continue
        names = _annotation_event_names(ann)
        if names:
            return names
    return []


# ----- graph builder ---------------------------------------------------------
class _LlamaIndexASTVisitor:
    """Extractor over a parsed module.

    Collects the FIRST ``Workflow`` subclass and its ``@step`` methods, then
    builds ``producers[event]`` / ``consumers[event]`` maps and cross-products
    them into step→step edges. ``StartEvent`` consumer ⇒ entry; ``StopEvent``
    producer ⇒ has_exit_branch. Sentinels are never nodes.
    """

    def __init__(self, source_path: str) -> None:
        self._source_path = source_path
        # Ordered list of (step_name, FunctionDef) for the chosen workflow.
        self._steps: List[Tuple[str, Any]] = []

    def collect(self, tree: _ast.Module) -> None:
        # Find the first top-level (or nested) Workflow subclass with @step
        # methods. Scope steps to ONE class so unrelated workflows sharing event
        # names don't cross-link.
        chosen: Optional[_ast.ClassDef] = None
        for node in _ast.walk(tree):
            if isinstance(node, _ast.ClassDef) and _is_workflow_class(node):
                if any(_is_step_method(item) for item in node.body):
                    chosen = node
                    break
        if chosen is None:
            return
        seen: Set[str] = set()
        for item in chosen.body:
            if _is_step_method(item) and item.name not in seen:
                seen.add(item.name)
                self._steps.append((item.name, item))

    def build(self) -> Dict[str, Any]:
        step_names = [name for name, _ in self._steps]
        step_set = set(step_names)

        # producers[event] = list of step names whose RETURN annotation includes
        # the event; consumers[event] = list of step names whose CONSUMED event
        # is the event. Both sentinel events (Start/Stop) are tracked separately.
        producers: Dict[str, List[str]] = {}
        consumers: Dict[str, List[str]] = {}
        exit_nodes: Set[str] = set()
        start_consumers: List[str] = []

        for name, func in self._steps:
            # Produced events (return annotation, possibly a union).
            for ev in _annotation_event_names(func.returns):
                if ev in _STOP_EVENT_NAMES:
                    exit_nodes.add(name)
                    continue
                if ev in _START_EVENT_NAMES:
                    # A step that *returns* StartEvent is unusual; StartEvent is
                    # the entry signal, not a routable inter-step event. Ignore.
                    continue
                if ev in _BASE_EVENT_NAMES:
                    # Bare ``Event`` is unresolvable — omit, never invent.
                    continue
                producers.setdefault(ev, [])
                if name not in producers[ev]:
                    producers[ev].append(name)
            # Consumed event(s) (param annotation, possibly a union).
            for ev in _consumed_events(func):
                if ev in _START_EVENT_NAMES:
                    if name not in start_consumers:
                        start_consumers.append(name)
                    continue
                if ev in _STOP_EVENT_NAMES:
                    # Consuming StopEvent is not a real pattern; ignore.
                    continue
                if ev in _BASE_EVENT_NAMES:
                    # Bare ``Event`` is unresolvable — omit, never invent.
                    continue
                consumers.setdefault(ev, [])
                if name not in consumers[ev]:
                    consumers[ev].append(name)

        # Cross-product producers × consumers per shared event type → edges.
        out_edges: List[Dict[str, Any]] = []
        seen_edges: Set[Tuple[str, str]] = set()
        for ev, prods in producers.items():
            cons = consumers.get(ev)
            if not cons:
                # Produced but never statically consumed (dynamic dispatch, or a
                # terminal custom event) — omit the edge rather than invent one.
                continue
            for p in prods:
                for c in cons:
                    key = (p, c)
                    if key in seen_edges:
                        continue
                    seen_edges.add(key)
                    out_edges.append({"from": p, "to": c})

        out_nodes: List[Dict[str, Any]] = []
        for name, func in self._steps:
            out_nodes.append({
                "id":   name,
                "name": name,
                # Every step is a leaf unit of agent work whose body we don't
                # introspect → NodeTypeTask. This keeps the cycle-with-StopEvent
                # fixture downgrading to Warning (HasExitBranch parity) and the
                # tool-oriented rules from false-positiving on arbitrary bodies.
                "type": "task",
                "config": {},
                "pos": {
                    "file": self._source_path,
                    "line": getattr(func, "lineno", 0),
                    "col":  getattr(func, "col_offset", 0),
                },
                "has_exit_branch": name in exit_nodes,
            })

        entry, ambiguous = self._infer_entry(step_names, start_consumers)

        metadata: Dict[str, Any] = {
            "source_format": "llamaindex",
            "source_file":   self._source_path,
            "llamaindex_version": "ast",
            "extraction":    "ast",
            "conditional_edge_reason": "exact_static_match",
        }
        if ambiguous:
            metadata["entry_ambiguous"] = True

        _ = step_set  # (retained for clarity; edges already scoped to steps)

        return {
            "nodes": out_nodes,
            "edges": out_edges,
            # When the entry is ambiguous (no StartEvent consumer, or more than
            # one), leave entry_node_id empty and signal entry_ambiguous at the
            # TOP LEVEL so decodeShimGraph propagates EntryAmbiguous and
            # reachability SKIPS the graph rather than reporting false
            # unreachable_node positives.
            "entry_node_id": "" if ambiguous else entry,
            "entry_ambiguous": ambiguous,
            "metadata": metadata,
        }

    def _infer_entry(self, step_names: List[str], start_consumers: List[str]) -> Tuple[str, bool]:
        if not step_names:
            return "", False
        # Entry = the unique StartEvent consumer. Zero or >1 ⇒ ambiguous.
        if len(start_consumers) == 1:
            return start_consumers[0], False
        if len(start_consumers) == 0:
            # No StartEvent consumer found (dynamic entry, or unannotated) —
            # ambiguous; reachability skips. Fall back to first step for a
            # deterministic (but flagged) value.
            return step_names[0], True
        # More than one StartEvent consumer — genuinely ambiguous entry.
        return start_consumers[0], True


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
    visitor = _LlamaIndexASTVisitor(source_path)
    visitor.collect(tree)
    return visitor.build()


def _empty_graph(source_path: str) -> Dict[str, Any]:
    return {
        "nodes": [],
        "edges": [],
        "entry_node_id": "",
        "metadata": {
            "source_format": "llamaindex",
            "source_file":   source_path,
            "llamaindex_version": "ast",
            "extraction":    "ast_empty",
            "conditional_edge_reason": "exact_static_match",
        },
    }


# ----- handlers --------------------------------------------------------------
def _handle_health_check(_: Dict[str, Any]) -> Dict[str, Any]:
    # AST-only: we never import llama_index, so health is "ok" whenever Python
    # itself loaded this shim. The version string is the sentinel "ast" to make
    # the strategy explicit in diagnostics.
    return {
        "status": "ok",
        "llamaindex_version": "ast",
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
