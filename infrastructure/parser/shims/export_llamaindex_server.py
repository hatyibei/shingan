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
  * Human-in-the-loop (HITL) events — ``HumanResponseEvent`` /
    ``InputRequiredEvent`` (and direct subclasses, base-leaf aware like the
    StartEvent/StopEvent handling) — are EXTERNALLY INJECTABLE: the ``run()``
    driver feeds a ``HumanResponseEvent`` into the graph via
    ``ctx.send_event(HumanResponseEvent(...))``, so NO ``@step`` need produce it.
    A step consuming such an event would otherwise look unreachable (no producer
    edge); we model the external producer by connecting it from the ENTRY node
    — but ONLY when the step has no real in-edge already (a genuine ``@step``
    producer always wins, so this never fabricates a spurious cycle). This is
    exactly the botextractai ``get_feedback`` shape. Mirrors the
    StartEvent/StopEvent direct-subclass-only contract.
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
import os
import sys
import traceback
from typing import Any, Dict, List, Optional, Set, Tuple

# ----- stdout/stderr discipline ---------------------------------------------
# Keep stdout reserved for response frames; route any stray print() to stderr.
# Duplicate the real stdout (fd 1) onto a private descriptor for JSON-RPC
# frames, then point fd 1 itself at stderr so even a C extension writing
# straight to fd 1 (bypassing Python) can't corrupt the response stream.
_RESPONSE_FD = os.dup(1)
os.dup2(2, 1)
sys.stdout = sys.stderr  # python-level stray print() also lands on stderr.
_RESPONSE_STREAM = os.fdopen(_RESPONSE_FD, "w", encoding="utf-8")


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
# Human-in-the-loop events provided by ``llama_index.core.workflow``. These are
# injected EXTERNALLY by the run() driver (``ctx.send_event(HumanResponseEvent(
# ...))``) rather than produced by any ``@step``, so a step consuming one is
# reachable at runtime even though the static producer/consumer match finds no
# producer. Recognised by leaf name (the framework is never imported), with
# direct subclasses widening the set in ``collect()`` — mirroring the
# StartEvent/StopEvent subclass handling.
_HITL_EVENT_NAMES = {"HumanResponseEvent", "InputRequiredEvent"}
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

# Aliased imports — `from llama_index... import StartEvent as SE` /
# `Workflow as WF` / `step as li_step` — would defeat the leaf-name matching
# above (codex review 2026-05-31: aliased source silently produced empty
# graphs). _ALIASES maps `<alias> -> <framework symbol>` for the current parse;
# _canon() resolves a leaf name back through it before every framework-name
# comparison. Populated per-parse by _try_extract (the worker is single-threaded
# per request, so a module-level map is safe).
_FRAMEWORK_NAMES = frozenset(
    _START_EVENT_NAMES | _STOP_EVENT_NAMES | _BASE_EVENT_NAMES
    | _HITL_EVENT_NAMES | _CONTEXT_TYPE_NAMES | {_STEP_DECORATOR, _WORKFLOW_BASE}
)
_ALIASES: Dict[str, str] = {}


def _canon(name: Optional[str]) -> Optional[str]:
    """Resolve an import alias back to the framework symbol it names."""
    if name is None:
        return None
    return _ALIASES.get(name, name)


def _collect_aliases(tree: _ast.Module) -> Dict[str, str]:
    """Map `<alias> -> <framework symbol>` from `import X as alias` /
    `from M import X as alias` where X is a framework name."""
    aliases: Dict[str, str] = {}
    for node in _ast.walk(tree):
        if isinstance(node, (_ast.Import, _ast.ImportFrom)):
            for al in node.names:
                if al.asname and al.name in _FRAMEWORK_NAMES:
                    aliases[al.asname] = al.name
    return aliases


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
        if _canon(_base_name(deco)) == _STEP_DECORATOR:
            return True
    return False


def _is_workflow_class(cls: _ast.ClassDef) -> bool:
    """True when the class subclasses ``Workflow`` (directly, by any spelling
    _base_name understands). Leaf-name match — the framework is never loaded.
    """
    for base in cls.bases:
        if _canon(_base_name(base)) == _WORKFLOW_BASE:
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
    out). Skips ``self`` and the ``Context`` param (recognised by the
    conventional name ``ctx``/``context`` FIRST — so a project-local alias such
    as ``ctx: AnyContext`` is skipped — with the literal ``Context`` annotation
    leaf as a fallback). The consumed event is the first non-self, non-Context
    positional param that carries a resolvable annotation.
    """
    args = func.args
    # Include kwonlyargs: a step may declare its event keyword-only
    # (`async def run(self, *, ev: StartEvent)`) — codex review 2026-05-31.
    params = list(args.posonlyargs) + list(args.args) + list(args.kwonlyargs)
    for arg in params:
        if arg.arg == "self":
            continue
        ann = arg.annotation
        # Skip the Context param. Name FIRST, regardless of annotation: steps
        # are written `def step(self, ctx: <T>, ev: SomeEvent)` and the context
        # is conventionally named ``ctx``/``context``. Its annotation is often a
        # project-local alias (`ctx: AnyContext` where `AnyContext = Context`)
        # rather than the literal ``Context``; relying on the annotation leaf
        # alone mis-reads such a ctx param as the consumed event (wild dogfood:
        # zylon-ai/private-gpt → 0 edges, entry_ambiguous). The conventional
        # name is the reliable signal — event params are `ev`/`event`, never
        # `ctx`/`context`.
        if arg.arg in _CONTEXT_PARAM_NAMES:
            continue
        # Annotation-leaf fallback: an unconventionally named context param
        # still annotated with the literal ``Context`` (or an aliased import).
        if ann is not None and _canon(_leaf_name(ann)) in _CONTEXT_TYPE_NAMES:
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
        # User-defined subclasses of StartEvent / StopEvent. LlamaIndex code
        # frequently defines a concrete entry/exit event by subclassing the
        # sentinel (`class ImageInputEvent(StartEvent)` / `...(StopEvent)`) and
        # annotating steps with the subclass, not the bare sentinel (wild
        # dogfood: zylon-ai/private-gpt). Treat such subclass leaf names as the
        # sentinel they extend so entry + has_exit_branch still resolve.
        # Direct-subclass-only, matching the existing direct-Workflow-subclass
        # contract (docstring "Only DIRECT Workflow subclasses are detected").
        self._start_subclasses: Set[str] = set()
        self._stop_subclasses: Set[str] = set()
        # Direct subclasses of the HITL events (``class Approval(
        # HumanResponseEvent)``). Treated as externally injectable like their
        # base, so a step consuming the subclass is not flagged unreachable.
        # Direct-subclass-only, matching the StartEvent/StopEvent contract.
        self._hitl_subclasses: Set[str] = set()

    def collect(self, tree: _ast.Module) -> None:
        # Gather direct StartEvent/StopEvent subclasses across the module BEFORE
        # choosing the workflow, so the produce/consume scan in build() can map
        # an annotation like `-> ImageProcessingResultEvent` back to its
        # StopEvent base. (Event classes are typically module-level siblings of
        # the workflow class, not nested in it.)
        for node in _ast.walk(tree):
            if not isinstance(node, _ast.ClassDef):
                continue
            for base in node.bases:
                leaf = _canon(_base_name(base))
                if leaf in _START_EVENT_NAMES:
                    self._start_subclasses.add(node.name)
                elif leaf in _STOP_EVENT_NAMES:
                    self._stop_subclasses.add(node.name)
                elif leaf in _HITL_EVENT_NAMES:
                    self._hitl_subclasses.add(node.name)

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
        # Steps that consume a HITL event (HumanResponseEvent / InputRequiredEvent
        # or a direct subclass). The event is injected externally by the run()
        # driver, so these are recorded for the synthetic entry→step edge below
        # — but the event ALSO stays in the normal ``consumers`` map, so if some
        # @step genuinely produces it the real producer edge still wins.
        hitl_consumers: Set[str] = set()

        # Sentinel sets widened by user-defined direct subclasses: a step
        # annotated with `ImageInputEvent(StartEvent)` consumes a StartEvent, and
        # one returning `ResultEvent(StopEvent)` produces a StopEvent. The
        # literal sentinels are always included.
        start_names = _START_EVENT_NAMES | self._start_subclasses
        stop_names = _STOP_EVENT_NAMES | self._stop_subclasses
        hitl_names = _HITL_EVENT_NAMES | self._hitl_subclasses

        for name, func in self._steps:
            # Produced events (return annotation, possibly a union).
            for ev in _annotation_event_names(func.returns):
                ev = _canon(ev)  # resolve aliased StartEvent/StopEvent
                if ev in stop_names:
                    exit_nodes.add(name)
                    continue
                if ev in start_names:
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
                ev = _canon(ev)  # resolve aliased StartEvent/StopEvent
                if ev in start_names:
                    if name not in start_consumers:
                        start_consumers.append(name)
                    continue
                if ev in stop_names:
                    # Consuming StopEvent is not a real pattern; ignore.
                    continue
                if ev in _BASE_EVENT_NAMES:
                    # Bare ``Event`` is unresolvable — omit, never invent.
                    continue
                if ev in hitl_names:
                    # Externally-injected HITL event: record the step so it can
                    # be wired from the entry below (typically no @step produces
                    # it). Fall through to ALSO add it to ``consumers`` — if some
                    # @step genuinely produces this event the real producer edge
                    # fires and gives the step an in-edge, which then SUPPRESSES
                    # the synthetic entry edge (precise: external producer only
                    # when there is no static one).
                    hitl_consumers.add(name)
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

        # Externally-injected HITL events: a step consuming HumanResponseEvent /
        # InputRequiredEvent (or a direct subclass) is reachable at runtime even
        # though no @step produces the event — the run() driver injects it via
        # ``ctx.send_event(...)``. Model that external producer as the ENTRY node
        # so reachability does not false-positive ``unreachable_node`` (real
        # example: botextractai ``get_feedback``). Two guards keep this precise:
        #   * Only when the entry resolved unambiguously (an ambiguous-entry graph
        #     already skips reachability, and we have no node to anchor to).
        #   * Only for a HITL step with NO existing in-edge: a genuine @step
        #     producer already gave it one, so we add nothing and never fabricate
        #     a spurious entry→step edge (which could invent a cycle).
        if not ambiguous and entry:
            targets = {e["to"] for e in out_edges}
            for c in step_names:
                if c not in hitl_consumers or c == entry or c in targets:
                    continue
                key = (entry, c)
                if key in seen_edges:
                    continue
                seen_edges.add(key)
                out_edges.append({"from": entry, "to": c})

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
    # Resolve import aliases (Workflow as WF, StartEvent as SE, …) so the
    # leaf-name matching recognises the framework symbols (codex review).
    global _ALIASES
    _ALIASES = _collect_aliases(tree)
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
