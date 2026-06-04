#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Long-lived JSON-RPC worker that exports Microsoft AutoGen GraphFlow workflow
definitions into Shingan's WorkflowGraph JSON format.

Strategy: AST-only (ADR-015 PoC, mirroring the pydantic-graph / LangGraph.js shims)
==================================================================================
This shim NEVER imports ``autogen_agentchat``. It parses the user's Python
source with the stdlib ``ast`` module and derives the workflow graph from the
static structure of a ``DiGraphBuilder`` / ``GraphFlow`` construction:

  * Nodes are agents registered via ``builder.add_node(agent)``. The agent
    reference is usually a variable holding an ``AssistantAgent(name="X", ...)``.
    The node id/name is the agent's ``name=`` string when statically
    resolvable (we resolve the variable → its ``AssistantAgent(name=...)``),
    else the bare variable name.
  * Edges come from ``builder.add_edge(A, B)`` → A→B. A conditional edge —
    ``builder.add_edge(A, B, condition="keyword")`` (3rd positional or a
    ``condition=`` kwarg) — still produces the A→B edge; the condition string,
    when a static constant, is carried on ``Edge.condition``. The EDGE is
    emitted regardless of whether the condition string resolves — dropping a
    conditional edge would hide the very structural exit that downgrades a
    loop from Critical to Warning (see ADR-015 / cycle.go cycleHasExit).
  * Entry = ``builder.set_entry_point(X)`` when present, else the single node
    with no incoming edges. Multiple / no zero-in-degree roots ⇒ entry is
    ambiguous (top-level ``entry_ambiguous=true``, like the pydantic-graph shim).

IMPORTANT — AutoGen has NO in-graph exit sentinel
-------------------------------------------------
Unlike LangGraph (``END``), pydantic-graph (``End``) or LlamaIndex
(``StopEvent``), AutoGen GraphFlow terminates via EXTERNAL termination
conditions (``MaxMessageTermination``, ``TextMentionTermination``) passed to
``GraphFlow``, NOT an in-graph sentinel. So this shim invents no sentinel and
does not set ``has_exit_branch`` from a fake END. Cycle classification relies
on shingan's STRUCTURAL cycle-exit (cycle.go ``cycleHasExit``): a cycle that
has an edge leaving the cycle to another real node is downgraded to Warning. A
pure loop with no structural exit edge stays Critical — accepted as a known
modelling boundary for this PoC.

Because the analysis is purely static, there is no "framework not installed"
gate — Python 3 alone is sufficient, exactly like the AST-only Node shim.

Protocol
========
Newline-delimited JSON (one request per line on stdin, one response per line
on stdout). All log output and tracebacks go to stderr — stdout MUST contain
only response frames.

Methods
-------
- ``health_check``   → ``{"status": "ok", "autogen_version": "ast"}``
                      (always "ok" when Python loads; never imports the lib)
- ``parse_file``     → ``{"path": <abs path>}`` → WorkflowGraph JSON
- ``parse_content``  → ``{"content": <python source>, "filename": <hint>}``
                      → WorkflowGraph JSON
- ``shutdown``       → ``{"ok": true}`` then process exits.

Robust: syntax errors / non-AutoGen files → empty graph, never crash the worker.

ADR references
--------------
- ADR-015: framework-by-framework PoC parsers (AutoGen after LlamaIndex).
- ADR-014: AST-primary extraction to sidestep the runtime-introspection trap.
- ADR-009: long-lived worker (no per-file fork) + degraded mode.
"""
from __future__ import annotations

import ast as _ast
import json
import os
import sys
import traceback
from typing import Any, Dict, List, Optional, Tuple

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


# ----- AST helpers -----------------------------------------------------------
# Builder method names we understand. ``add_conditional_edges`` is deliberately
# NOT handled: its signature varies across AutoGen versions and a wrong guess
# would emit phantom edges. We omit rather than invent (ADR-015 contract).
_ADD_NODE_NAMES = {"add_node"}
_ADD_EDGE_NAMES = {"add_edge"}
_ENTRY_NAMES = {"set_entry_point"}
# Constructors whose method calls we trust as AutoGen graph edits. Without
# this gate, ANY `*.add_node`/`*.add_edge` (e.g. NetworkX `g.add_edge(a, b)`)
# would be mistaken for an AutoGen graph and emit phantom nodes / cycle
# findings (codex review 2026-05-31, P1).
_BUILDER_CTOR_NAMES = {"DiGraphBuilder"}


def _is_builder_ctor_name(name: Optional[str]) -> bool:
    return bool(name) and (name in _BUILDER_CTOR_NAMES or name.endswith("GraphBuilder"))

# Agent constructor leaf names whose ``name=`` kwarg gives the node id. We match
# liberally on any ``*Agent`` constructor so AssistantAgent / UserProxyAgent /
# custom ``FooAgent`` subclasses all resolve, but require an explicit
# ``name="..."`` constant — a computed name degrades to the variable name.
def _is_agent_ctor_name(name: Optional[str]) -> bool:
    return bool(name) and name.endswith("Agent")


def _attr_leaf(expr: _ast.expr) -> Optional[str]:
    """Return the bare attribute/name leaf of a call target:
        builder.add_node        → "add_node"        (Attribute)
        add_node                → "add_node"        (Name)
    """
    if isinstance(expr, _ast.Attribute):
        return expr.attr
    if isinstance(expr, _ast.Name):
        return expr.id
    return None


def _func_leaf_name(call: _ast.Call) -> Optional[str]:
    """The constructor / function leaf name of a Call.
        AssistantAgent(...)     → "AssistantAgent"
        autogen.AssistantAgent  → "AssistantAgent"
    """
    func = call.func
    if isinstance(func, _ast.Name):
        return func.id
    if isinstance(func, _ast.Attribute):
        return func.attr
    return None


def _const_str(expr: Optional[_ast.expr]) -> Optional[str]:
    """Return the string value of a constant expression, else None."""
    if isinstance(expr, _ast.Constant) and isinstance(expr.value, str):
        return expr.value
    return None


def _agent_name_from_ctor(call: _ast.Call) -> Optional[str]:
    """Given an ``AssistantAgent(name="researcher", ...)`` Call, return the
    static ``name=`` string. A positional first-arg string also counts
    (``AssistantAgent("researcher")``). None when the name is computed.
    """
    for kw in call.keywords:
        if kw.arg == "name":
            return _const_str(kw.value)
    # Some agents take name as the first positional arg.
    if call.args:
        return _const_str(call.args[0])
    return None


def _safe_id(name: str) -> str:
    out: List[str] = []
    for ch in str(name).strip():
        if ch.isalnum() or ch in "_-./":
            out.append(ch)
        elif ch.isspace():
            out.append("_")
    return "".join(out).strip("_") or "node"


# ----- graph builder ---------------------------------------------------------
class _AutoGenASTVisitor:
    """Two-pass extractor over a parsed module.

    Pass 1 (assignment pass) records every ``var = AssistantAgent(name="X")``
    binding so ``add_node(var)`` resolves to "X".

    Pass 2 walks every ``builder.add_node`` / ``builder.add_edge`` /
    ``builder.set_entry_point`` call, resolving each agent reference through the
    same binding map. Edges are emitted for both plain and conditional
    ``add_edge`` forms; the condition string is attached when statically a
    constant. Unknown references degrade to the bare variable name; we never
    invent a node that wasn't added.
    """

    def __init__(self, source_path: str) -> None:
        self._source_path = source_path
        # variable name → resolved agent name (from AssistantAgent(name=...)).
        self._var_to_agent: Dict[str, str] = {}
        # variable name → (lineno, col) of the binding, for node SourcePos.
        self._var_pos: Dict[str, Tuple[int, int]] = {}
        # node id → (lineno, col); declaration order preserved in _order.
        self._node_pos: Dict[str, Tuple[int, int]] = {}
        self._order: List[str] = []
        # edges as (from, to, condition).
        self._edges: List[Tuple[str, str, Optional[str]]] = []
        # explicit entry node names (set_entry_point), in declaration order.
        self._explicit_entry: List[str] = []
        # variables bound to a DiGraphBuilder() — only calls on these (or an
        # inline/fluent DiGraphBuilder() chain) are treated as graph edits.
        self._builder_vars: set = set()
        # for-loop / comprehension control-variable names. A name used as a
        # loop target is an iteration placeholder, NOT a stable agent binding:
        # in ``for agent in (n1, n2, ...): builder.add_node(agent)`` the only
        # ``add_node`` call carries the loop variable, so registering it would
        # emit a phantom node literally named "agent". The REAL nodes are
        # recovered from the per-endpoint ``add_edge`` references (each edge
        # endpoint is registered via _register_node), so dropping the loop
        # variable loses nothing as long as every node also appears on an edge.
        # Such a bare Name, when it was never bound to an agent ctor, is dropped
        # in _resolve_ref. NOTE: this set is flat/module-wide — a name used as a
        # loop target anywhere suppresses that bare identifier as a node
        # everywhere; verified harmless on the wild targets (agent vars never
        # collide with loop-variable names).
        self._loop_targets: set = set()

    # ---- Pass 1: assignment / agent-binding pass ---------------------------
    def collect_bindings(self, tree: _ast.Module) -> None:
        for node in _ast.walk(tree):
            self._collect_loop_targets(node)
            if not isinstance(node, _ast.Assign):
                continue
            if not isinstance(node.value, _ast.Call):
                continue
            ctor = _func_leaf_name(node.value)
            if _is_builder_ctor_name(ctor):
                for tgt in node.targets:
                    if isinstance(tgt, _ast.Name):
                        self._builder_vars.add(tgt.id)
                continue
            if not _is_agent_ctor_name(ctor):
                continue
            agent_name = _agent_name_from_ctor(node.value)
            for tgt in node.targets:
                if isinstance(tgt, _ast.Name):
                    pos = (getattr(node, "lineno", 0), getattr(node, "col_offset", 0))
                    self._var_pos[tgt.id] = pos
                    if agent_name is not None:
                        self._var_to_agent[tgt.id] = agent_name

    def _collect_loop_targets(self, node: _ast.AST) -> None:
        """Record names bound as for-loop / comprehension control targets so a
        bare reference to the iteration placeholder is not mistaken for a real
        agent node (kills the phantom ``add_node(loop_var)`` node)."""
        targets: List[_ast.expr] = []
        if isinstance(node, (_ast.For, _ast.AsyncFor)):
            targets.append(node.target)
        elif isinstance(node, (_ast.ListComp, _ast.SetComp, _ast.GeneratorExp, _ast.DictComp)):
            for gen in node.generators:
                targets.append(gen.target)
        for tgt in targets:
            for name in _ast.walk(tgt):
                if isinstance(name, _ast.Name):
                    self._loop_targets.add(name.id)

    # ---- Pass 2: builder call walk -----------------------------------------
    def collect_builder_calls(self, tree: _ast.Module) -> None:
        for node in _ast.walk(tree):
            if not isinstance(node, _ast.Call):
                continue
            method = _attr_leaf(node.func)
            if (method not in _ADD_NODE_NAMES and method not in _ADD_EDGE_NAMES
                    and method not in _ENTRY_NAMES):
                continue
            # P1: only trust graph edits on a DiGraphBuilder receiver, so an
            # unrelated builder-pattern API (NetworkX etc.) isn't parsed as an
            # AutoGen graph.
            if not self._is_builder_receiver(node.func):
                continue
            if method in _ADD_NODE_NAMES:
                self._handle_add_node(node)
            elif method in _ADD_EDGE_NAMES:
                self._handle_add_edge(node)
            elif method in _ENTRY_NAMES:
                self._handle_set_entry(node)

    def _is_builder_receiver(self, func: _ast.expr) -> bool:
        """True when a call's receiver traces to a DiGraphBuilder — a tracked
        builder variable, an inline `DiGraphBuilder()`, or a fluent chain on
        one (`builder.add_node(a).add_edge(...)`)."""
        if not isinstance(func, _ast.Attribute):
            return False
        return self._traces_to_builder(func.value)

    def _traces_to_builder(self, expr: _ast.expr) -> bool:
        if isinstance(expr, _ast.Name):
            return expr.id in self._builder_vars
        if isinstance(expr, _ast.Call):
            if _is_builder_ctor_name(_func_leaf_name(expr)):
                return True
            if isinstance(expr.func, _ast.Attribute):
                return self._traces_to_builder(expr.func.value)
        return False

    @staticmethod
    def _arg(call: _ast.Call, index: int, *kwnames: str) -> Optional[_ast.expr]:
        """Positional arg at `index`, else the first matching keyword arg."""
        if len(call.args) > index:
            return call.args[index]
        for kw in call.keywords:
            if kw.arg in kwnames:
                return kw.value
        return None

    def _resolve_ref(self, expr: _ast.expr) -> Optional[Tuple[str, Tuple[int, int]]]:
        """Resolve an agent reference to (node_id, (lineno, col)).

        Resolution order:
          1. inline ``AssistantAgent(name="X")`` → X
          2. variable bound to an AssistantAgent(name="X") in pass 1 → X
          3. attribute access ``self.user_proxy`` → the trailing attr name
             (``user_proxy``), with the same _var_to_agent fallback — this is
             the canonical class-based DiGraphBuilder idiom (instance attrs hold
             the agents). Only called from add_node/add_edge/set_entry_point, so
             a self.<attr> that is never a graph argument is never invented.
          4. bare variable name (the fallback when name= is computed/missing)
        A bare Name that is a for-loop / comprehension control target and was
        never bound to an agent ctor is dropped (returns None): it is the
        iteration placeholder, not a stable agent — registering it would emit a
        phantom node literally named after the loop variable.
        Returns None for references we can't name so the caller can skip rather
        than invent.
        """
        # Inline agent construction: add_node(AssistantAgent(name="X")).
        if isinstance(expr, _ast.Call):
            ctor = _func_leaf_name(expr)
            if _is_agent_ctor_name(ctor):
                nm = _agent_name_from_ctor(expr)
                pos = (getattr(expr, "lineno", 0), getattr(expr, "col_offset", 0))
                if nm is not None:
                    return nm, pos
            return None
        # Attribute access: ``self.user_proxy`` / ``self.extractor`` — resolve
        # to the trailing attr name. The class-based idiom binds agents to
        # instance attributes (``self.user_proxy = ...``) and adds them as
        # ``builder.add_node(self.user_proxy)``; without this the whole graph is
        # dropped. We honour _var_to_agent on the attr name when an inline
        # ``self.x = SomeAgent(name="X")`` was statically resolvable.
        if isinstance(expr, _ast.Attribute):
            attr = expr.attr
            pos = self._var_pos.get(attr, (getattr(expr, "lineno", 0), getattr(expr, "col_offset", 0)))
            resolved = self._var_to_agent.get(attr, attr)
            return resolved, pos
        if isinstance(expr, _ast.Name):
            var = expr.id
            # An unbound loop / comprehension control variable is an iteration
            # placeholder, never a real node. Drop it so a phantom node named
            # after the loop variable is not registered.
            if var in self._loop_targets and var not in self._var_to_agent:
                return None
            pos = self._var_pos.get(var, (getattr(expr, "lineno", 0), getattr(expr, "col_offset", 0)))
            resolved = self._var_to_agent.get(var, var)
            return resolved, pos
        return None

    def _register_node(self, node_id: str, pos: Tuple[int, int]) -> None:
        if node_id not in self._node_pos:
            self._node_pos[node_id] = pos
            self._order.append(node_id)

    def _handle_add_node(self, call: _ast.Call) -> None:
        arg = self._arg(call, 0, "node", "agent")
        if arg is None:
            return
        ref = self._resolve_ref(arg)
        if ref is None:
            return
        node_id, pos = ref
        self._register_node(node_id, pos)

    def _handle_add_edge(self, call: _ast.Call) -> None:
        # Positional or keyword (`add_edge(source=a, target=b)`) — codex P2.
        src_arg = self._arg(call, 0, "source", "from_node")
        dst_arg = self._arg(call, 1, "target", "to_node")
        if src_arg is None or dst_arg is None:
            return
        src = self._resolve_ref(src_arg)
        dst = self._resolve_ref(dst_arg)
        if src is None or dst is None:
            return
        src_id, src_pos = src
        dst_id, dst_pos = dst
        # An edge can be drawn between agents that were also add_node'd; if a
        # builder draws an edge to an agent it never explicitly add_node'd we
        # still register the endpoint so the edge is not dangling (AutoGen
        # build() infers participants from edges in some versions).
        self._register_node(src_id, src_pos)
        self._register_node(dst_id, dst_pos)
        # Condition: 3rd positional arg or a `condition=`/`activation=` kwarg.
        condition = self._edge_condition(call)
        self._edges.append((src_id, dst_id, condition))

    @staticmethod
    def _edge_condition(call: _ast.Call) -> Optional[str]:
        if len(call.args) >= 3:
            c = _const_str(call.args[2])
            if c is not None:
                return c
        for kw in call.keywords:
            if kw.arg in ("condition", "activation_condition"):
                c = _const_str(kw.value)
                if c is not None:
                    return c
        return None

    def _handle_set_entry(self, call: _ast.Call) -> None:
        arg = self._arg(call, 0, "node", "agent")
        if arg is None:
            return
        ref = self._resolve_ref(arg)
        if ref is None:
            return
        node_id, _pos = ref
        self._explicit_entry.append(node_id)

    # ---- build the WorkflowGraph dict --------------------------------------
    def build(self) -> Dict[str, Any]:
        node_names = list(self._order)
        node_set = set(node_names)

        out_nodes: List[Dict[str, Any]] = []
        out_edges: List[Dict[str, Any]] = []
        # Count incoming edges per node for entry inference. Self-loops do not
        # contribute (a loop-only node is still a valid root).
        real_indeg: Dict[str, int] = {n: 0 for n in node_names}
        # Dedupe identical (from, to) pairs while preserving the first condition.
        seen_edges: set = set()

        for src_id, dst_id, condition in self._edges:
            if src_id not in node_set or dst_id not in node_set:
                continue
            key = (src_id, dst_id)
            if key in seen_edges:
                continue
            seen_edges.add(key)
            edge: Dict[str, Any] = {"from": src_id, "to": dst_id}
            if condition:
                edge["condition"] = condition
            out_edges.append(edge)
            if dst_id != src_id:
                real_indeg[dst_id] = real_indeg.get(dst_id, 0) + 1

        for name in node_names:
            line, col = self._node_pos.get(name, (0, 0))
            out_nodes.append({
                "id":   name,
                "name": name,
                # An AutoGen agent node is a leaf unit of agent work whose body
                # we don't introspect → NodeTypeTask (parity with the
                # pydantic-graph shim). Crucially this is NOT a Loop node, so a
                # cycle with a structural exit edge downgrades to Warning via
                # cycleHasExit rather than diverting into the max_iterations
                # branch; and the tool-oriented rules don't false-positive on
                # arbitrary agent bodies.
                "type": "task",
                "config": {},
                "pos": {
                    "file": self._source_path,
                    "line": line,
                    "col":  col,
                },
                # AutoGen has no in-graph exit sentinel; we never synthesise
                # one. Cycle bounding relies on the structural exit edge.
                "has_exit_branch": False,
            })

        entry, ambiguous = self._infer_entry(node_names, real_indeg)

        metadata: Dict[str, Any] = {
            "source_format": "autogen",
            "source_file":   self._source_path,
            "autogen_version": "ast",
            "extraction":    "ast",
            "conditional_edge_reason": "exact_static_match",
        }
        if ambiguous:
            metadata["entry_ambiguous"] = True

        return {
            "nodes": out_nodes,
            "edges": out_edges,
            # When the entry is ambiguous (multiple/zero zero-in-degree roots
            # and no set_entry_point), leave entry_node_id empty and signal
            # entry_ambiguous at the TOP LEVEL so decodeShimGraph propagates
            # EntryAmbiguous and reachability skips the graph rather than
            # reporting non-chosen roots as unreachable false positives.
            "entry_node_id": "" if ambiguous else entry,
            "entry_ambiguous": ambiguous,
            "metadata": metadata,
        }

    def _infer_entry(self, node_names: List[str], real_indeg: Dict[str, int]) -> Tuple[str, bool]:
        if not node_names:
            return "", False
        # 1. Explicit set_entry_point wins.
        for s in self._explicit_entry:
            if s in node_names:
                return s, False
        # 2. The single node that is never a (non-self) destination.
        zero_in = [n for n in node_names if real_indeg.get(n, 0) == 0]
        if len(zero_in) == 1:
            return zero_in[0], False
        if len(zero_in) > 1:
            # Multiple roots — pick the first registered for determinism but
            # flag the ambiguity so reachability skips the graph.
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
    visitor = _AutoGenASTVisitor(source_path)
    visitor.collect_bindings(tree)
    visitor.collect_builder_calls(tree)
    return visitor.build()


def _empty_graph(source_path: str) -> Dict[str, Any]:
    return {
        "nodes": [],
        "edges": [],
        "entry_node_id": "",
        "metadata": {
            "source_format": "autogen",
            "source_file":   source_path,
            "autogen_version": "ast",
            "extraction":    "ast_empty",
            "conditional_edge_reason": "exact_static_match",
        },
    }


# ----- handlers --------------------------------------------------------------
def _handle_health_check(_: Dict[str, Any]) -> Dict[str, Any]:
    # AST-only: we never import autogen_agentchat, so health is "ok" whenever
    # Python itself loaded this shim. The version string is the sentinel "ast"
    # to make the strategy explicit in diagnostics.
    return {
        "status": "ok",
        "autogen_version": "ast",
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
