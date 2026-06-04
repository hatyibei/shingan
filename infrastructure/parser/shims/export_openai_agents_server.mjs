// Long-lived JSON-RPC worker that exports OpenAI Agents SDK (`@openai/agents`)
// multi-agent definitions into Shingan's WorkflowGraph JSON format, via the
// TypeScript Compiler API (AST-primary, ADR-015).
//
// Protocol (identical to the Mastra / LangGraph.js shims, see
// export_mastra_server.mjs)
// ===========================================================================
// Newline-delimited JSON on stdin/stdout. One request per line on stdin, one
// response per line on stdout. ALL diagnostics / library chatter go to stderr;
// stdout MUST contain only response frames or the Go Call() framing breaks.
//
// Request:  {"id": <int|str>, "method": "<name>", "params": {...}}
// Success:  {"id": <id>, "result": <any>}
// Error:    {"id": <id>, "error": {"code": <int>, "message": "<text>"}}
//
// Methods
// -------
// - health_check   -> {"status":"ok","openai_agents_version":"unknown"}
//                     The shim is AST-only; it never imports @openai/agents, so
//                     health is "ok" whenever node + the bundled TS parser load.
// - parse_file     -> {"path": <abs path>} -> WorkflowGraph JSON
// - parse_content  -> {"content": <ts/js source>, "filename": <hint>} -> WorkflowGraph JSON
// - shutdown       -> {"ok": true} then process exits.
//
// On parse failure a structured JSON-RPC error is returned (graceful
// degradation); the worker never crashes. A syntax-error / non-Agents file
// yields an empty graph (no nodes/edges), never an exception.
//
// OpenAI Agents model (what we extract)
// =====================================
// Agents:   const a = new Agent({ name: "Triage", handoffs: [b, handoff(c)] })
//           const t = Agent.create({ name: "Triage", handoffs: [billing] })
// Edges:    handoffs are DIRECTED edges agentA -> each resolved handoff target.
//           `handoffs: [b, handoff(c)]` => A->B and A->C. A `handoff(x, opts)`
//           wrapper is unwrapped to its inner agent identifier `x`.
//
// We resolve each `new Agent(...)` / `Agent.create(...)` binding (variable name
// -> the agent's `name` config, falling back to the binding name) into a node,
// then resolve every handoff array element to a DECLARED agent and emit one
// directed edge per resolved target.
//
// dest-must-be-declared gate (mirrors the LangGraph.js Command-goto gate)
// ----------------------------------------------------------------------
// A handoff target that does NOT resolve to a declared `new Agent`/`Agent.create`
// binding in this file (e.g. an agent imported from another module, or a
// computed value) yields NO edge — we omit it rather than fabricate a phantom
// node/edge. This is the OPPOSITE of the Mastra shim's best-effort
// `resolveStepRef` fallback: a Mastra `.then(importedStep)` legitimately names a
// cross-module step, but an unresolved OpenAI-Agents handoff target would
// corrupt cycle/reachability analysis with an invented node.
//
// Agents-as-tools (optional edges)
// --------------------------------
// `tools: [someAgent.asTool({...})]` turns an agent into a callable tool of the
// parent. When the `.asTool()` receiver resolves to a DECLARED agent we model it
// as a directed edge parent->thatAgent (same dest-must-be-declared gate). A
// `.asTool()` on an opaque/imported receiver, or a non-agent tool, is omitted.
//
// Entry & ambiguity
// -----------------
// Entry = the agent that is never a handoff target (zero in-degree), computed
// AFTER edge resolution. Exactly one root -> that is the entry. Two-or-more
// roots -> genuinely ambiguous: entry_ambiguous=true and entry_node_id="" (so
// reachability skips the graph rather than false-flagging non-chosen roots).
// Zero roots (a pure handoff cycle with every agent a target) -> fall back to
// the first declared agent so the graph still has an anchor.
//
// No exit sentinel
// ----------------
// OpenAI Agents has NO in-graph terminal sentinel — a run ends implicitly (final
// output / MaxTurnsExceededError / guardrail). Like AutoGen / Mastra, the shim
// invents no sentinel and NEVER sets has_exit_branch. Cycle bounding relies
// purely on shingan's structural cycle-exit (domain/rules/cycle.go
// `cycleHasExit`): a handoff loop with an edge leaving the cycle (e.g. an
// in-cycle agent that also hands off to a terminal agent) downgrades
// Critical -> Warning; a pure handoff loop with no structural exit stays
// Critical (documented PoC boundary, mirrors AutoGen).

import * as ts from "typescript";
import * as fs from "fs";
import * as readline from "readline";

// ----- node-id normalisation (mirrors the Mastra / LangGraph.js shim nodeId) --
function nodeId(name) {
  const s = String(name);
  const cleaned = s.trim();
  const out = [];
  let inWs = false;
  for (const ch of cleaned) {
    if (/\s/.test(ch)) {
      if (!inWs && out.length) out.push("_");
      inWs = true;
    } else {
      out.push(ch);
      inWs = false;
    }
  }
  const final = out.join("").replace(/^_+|_+$/g, "");
  return final || "node";
}

// Extract a plain string from a string-literal AST node, else null.
function stringOf(arg) {
  if (arg && ts.isStringLiteralLike(arg)) return arg.text;
  return null;
}

// Position helper: 1-based line/col of an AST node.
function posOf(sourceFile, node, filePath) {
  try {
    const { line, character } = sourceFile.getLineAndCharacterOfPosition(
      node.getStart(sourceFile)
    );
    return { file: filePath, line: line + 1, col: character + 1 };
  } catch (_e) {
    return { file: filePath, line: 0, col: 0 };
  }
}

// The Agent constructor / `handoff` helper must be the bindings IMPORTED from
// `@openai/agents` (and its subpaths, e.g. `@openai/agents/realtime`) — not any
// local class that happens to be named Agent. Returns alias-aware local-name
// sets. When the file imports neither, both are empty and nothing matches ->
// empty graph. Mirrors the Mastra shim's `collectMastraCtors` hardening.
function collectAgentBindings(sourceFile) {
  const agent = new Set(); // local names that mean `Agent`
  const handoffFn = new Set(); // local names that mean `handoff`
  const namespace = new Set(); // local names of `import * as X from "@openai/agents"`
  for (const stmt of sourceFile.statements) {
    if (!ts.isImportDeclaration(stmt)) continue;
    const spec = stmt.moduleSpecifier;
    if (!ts.isStringLiteralLike(spec)) continue;
    const mod = spec.text;
    if (mod !== "@openai/agents" && !mod.startsWith("@openai/agents/")) continue;
    const clause = stmt.importClause;
    if (!clause || !clause.namedBindings) continue;
    // `import * as oa from "@openai/agents"` → matched later only as oa.Agent /
    // oa.handoff (a namespace-qualified member), never a bare `foo.Agent`.
    if (ts.isNamespaceImport(clause.namedBindings)) {
      namespace.add(clause.namedBindings.name.text);
      continue;
    }
    if (!ts.isNamedImports(clause.namedBindings)) continue;
    for (const el of clause.namedBindings.elements) {
      const orig = el.propertyName ? el.propertyName.text : el.name.text;
      const local = el.name.text;
      if (orig === "Agent") agent.add(local);
      else if (orig === "handoff") handoffFn.add(local);
    }
  }
  return { agent, handoffFn, namespace };
}

// Resolve the static `name` string property of an Agent config object literal
// (`new Agent({ name: "x", ... })` / `Agent.create({ name: "x" })`). Returns the
// name string, or null when absent / non-literal.
function nameFromConfigObject(configObj) {
  if (!configObj || !ts.isObjectLiteralExpression(configObj)) return null;
  for (const prop of configObj.properties) {
    if (!ts.isPropertyAssignment(prop)) continue;
    let key = "";
    if (ts.isIdentifier(prop.name)) key = prop.name.text;
    else if (ts.isStringLiteralLike(prop.name)) key = prop.name.text;
    if (key === "name") {
      const s = stringOf(prop.initializer);
      if (s) return s;
    }
  }
  return null;
}

// Return the config object-literal argument of an Agent construction, or null.
//   new Agent({...})      -> NewExpression,  config = arguments[0]
//   Agent.create({...})   -> CallExpression, config = arguments[0]
// `isAgent` decides whether the callee/ctor names match an `@openai/agents`
// Agent binding (alias-aware). Both forms are recognised; the canonical triage
// example uses Agent.create specifically.
// `isAgentCtor(node)` decides whether an AST node is an `@openai/agents` Agent
// constructor/receiver: a bare named-import identifier, or a namespace-qualified
// `oa.Agent`. A plain `foo.Agent` (foo not an @openai/agents namespace import)
// must NOT match — that is some unrelated library's Agent (codex review #45).
function agentConfigObject(expr, isAgentCtor) {
  if (!expr) return null;
  // new Agent({...}) / new oa.Agent({...})
  if (ts.isNewExpression(expr)) {
    if (!isAgentCtor(expr.expression)) return null;
    const arg0 = expr.arguments && expr.arguments[0];
    return arg0 && ts.isObjectLiteralExpression(arg0) ? arg0 : null;
  }
  // Agent.create({...}) / oa.Agent.create({...}) — callee is `<AgentCtor>.create`
  if (ts.isCallExpression(expr) && ts.isPropertyAccessExpression(expr.expression)) {
    const pae = expr.expression;
    if (pae.name.text !== "create") return null;
    if (!isAgentCtor(pae.expression)) return null;
    const arg0 = expr.arguments && expr.arguments[0];
    return arg0 && ts.isObjectLiteralExpression(arg0) ? arg0 : null;
  }
  return null;
}

// ----- per-file accumulator ---------------------------------------------------
class GraphBuilder {
  constructor() {
    this.nodes = new Map(); // id -> {id,name,type,config,pos,has_exit_branch}
    this.edges = []; // {from,to}
    this._edgeKeys = new Set(); // dedupe "from to"
    this._order = []; // node ids in declaration order (entry fallback)
  }

  ensureNode(id, name, pos) {
    let n = this.nodes.get(id);
    if (!n) {
      n = {
        id,
        name: name || id,
        // Agent unit in a handoff/routing graph. NodeTypeTask (NOT llm/tool/loop)
        // keeps the tool / loop_guard / error_handler rules from false-
        // positiving and lets a handoff self/loop stay in the structural cycle
        // path (domain/rules/cycle.go). Mirrors the AutoGen GraphFlow shim's
        // agent-node choice (a pure agent routing graph, no tools as nodes).
        type: "task",
        config: {},
        pos: pos || { file: "", line: 0, col: 0 },
        // No exit sentinel in OpenAI Agents — never flagged (see header).
        has_exit_branch: false,
      };
      this.nodes.set(id, n);
      this._order.push(id);
    }
    return n;
  }

  pushEdge(from, to) {
    if (!from || !to) return;
    const key = from + " " + to;
    if (this._edgeKeys.has(key)) return;
    this._edgeKeys.add(key);
    this.edges.push({ from, to });
  }
}

// ----- the core extractor -----------------------------------------------------
function extract(content, filePath) {
  const sourceFile = ts.createSourceFile(
    filePath || "<inline.ts>",
    content,
    ts.ScriptTarget.Latest,
    /*setParentNodes*/ true,
    (filePath && /\.tsx$/.test(filePath)) ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  );

  // Resolve which local names mean `Agent` / `handoff` from the @openai/agents
  // imports (alias-aware). No such import -> empty sets -> nothing matches ->
  // empty graph.
  const bindings = collectAgentBindings(sourceFile);
  // An Agent constructor/receiver is a bare named-import identifier (`Agent`) OR
  // a member of an @openai/agents namespace import (`oa.Agent`). A `foo.Agent`
  // where foo is not such a namespace must NOT match (codex #45).
  const isAgentCtor = (node) => {
    if (ts.isIdentifier(node)) return bindings.agent.has(node.text);
    if (ts.isPropertyAccessExpression(node))
      return node.name.text === "Agent" &&
        ts.isIdentifier(node.expression) && bindings.namespace.has(node.expression.text);
    return false;
  };
  // `handoff(x)` callee: bare named import, or `oa.handoff(x)` namespace member.
  const isHandoffCallee = (node) => {
    if (ts.isIdentifier(node)) return bindings.handoffFn.has(node.text);
    if (ts.isPropertyAccessExpression(node))
      return node.name.text === "handoff" &&
        ts.isIdentifier(node.expression) && bindings.namespace.has(node.expression.text);
    return false;
  };

  // No @openai/agents Agent binding at all → not an Agents file → empty graph.
  // (handoff alone, without Agent, cannot declare a node.) A namespace import
  // also qualifies (oa.Agent), so consider both.
  if (bindings.agent.size === 0 && bindings.namespace.size === 0) {
    return {
      nodes: [],
      edges: [],
      entry_node_id: "",
      metadata: emptyMeta(filePath),
    };
  }

  const b = new GraphBuilder();

  // Pre-pass: map every `const x = new Agent({...})` / `const x = Agent.create(
  // {...})` variable name -> its node id (the config `name`, normalised, or the
  // binding name when no static name). This binding map is how handoff
  // references resolve. The same map drives the dest-must-be-declared gate:
  // a handoff identifier absent from it yields NO edge.
  const nodeIdByVar = new Map(); // varName -> node id
  const configByVar = new Map(); // varName -> Agent config object literal
  const collectAgents = (node) => {
    if (
      ts.isVariableDeclaration(node) &&
      node.name &&
      ts.isIdentifier(node.name) &&
      node.initializer
    ) {
      const cfg = agentConfigObject(node.initializer, isAgentCtor);
      if (cfg) {
        const varName = node.name.text;
        const declName = nameFromConfigObject(cfg);
        const id = nodeId(declName || varName);
        nodeIdByVar.set(varName, id);
        configByVar.set(varName, cfg);
        b.ensureNode(id, declName || varName, posOf(sourceFile, node, filePath));
      }
    }
    ts.forEachChild(node, collectAgents);
  };
  collectAgents(sourceFile);

  // If no agents were declared via a binding pattern we recognise, the file is
  // not an idiomatic Agents graph (e.g. only inline `new Agent(...)` returned
  // from a function). Degrade to empty rather than invent.
  if (b.nodes.size === 0) {
    return {
      nodes: [],
      edges: [],
      entry_node_id: "",
      metadata: emptyMeta(filePath),
    };
  }

  // Resolve a single handoff-array element to a DECLARED agent's node id, or
  // null. Supported forms:
  //   - identifier bound to a declared agent  -> its node id
  //   - handoff(agent) / handoff(agent, opts) -> unwrap, resolve the inner ident
  // Anything else (an inline `new Agent(...)`, a computed value, an identifier
  // not bound to a declared agent / imported from elsewhere) -> null (omitted).
  function resolveHandoffTarget(el) {
    if (!el) return null;
    if (ts.isIdentifier(el)) {
      const id = nodeIdByVar.get(el.text);
      return id || null; // dest-must-be-declared: unknown ident -> omit
    }
    // handoff(agentVar) / oa.handoff(agentVar, {...}) — unwrap to the inner agent.
    if (ts.isCallExpression(el) && isHandoffCallee(el.expression)) {
      const inner = el.arguments && el.arguments[0];
      if (inner && ts.isIdentifier(inner)) {
        const id = nodeIdByVar.get(inner.text);
        return id || null;
      }
    }
    return null;
  }

  // Resolve an `agentVar.asTool({...})` element to the receiver agent's node id,
  // or null. Only a DECLARED-agent receiver resolves (same dest gate). Used for
  // the optional agents-as-tools edges.
  function resolveAsToolReceiver(el) {
    if (!el) return null;
    if (
      ts.isCallExpression(el) &&
      ts.isPropertyAccessExpression(el.expression) &&
      el.expression.name.text === "asTool" &&
      ts.isIdentifier(el.expression.expression)
    ) {
      const id = nodeIdByVar.get(el.expression.expression.text);
      return id || null;
    }
    return null;
  }

  // Walk a config object's `handoffs` / `tools` array property and return its
  // array-literal element list, or null when absent / non-array (e.g. a
  // computed `handoffs: someVar` we cannot read — omitted per ADR-015).
  function arrayPropElements(configObj, key) {
    for (const prop of configObj.properties) {
      if (!ts.isPropertyAssignment(prop)) continue;
      let k = "";
      if (ts.isIdentifier(prop.name)) k = prop.name.text;
      else if (ts.isStringLiteralLike(prop.name)) k = prop.name.text;
      if (k !== key) continue;
      if (ts.isArrayLiteralExpression(prop.initializer)) return prop.initializer.elements;
      return null; // present but non-array (computed) -> omit
    }
    return null;
  }

  // Edge pass: for every declared agent, resolve its handoffs[] (and tools[]
  // agents-as-tools) to declared agents and emit directed edges. Edges to
  // unresolved targets are omitted (dest-must-be-declared).
  for (const [varName, cfg] of configByVar) {
    const fromId = nodeIdByVar.get(varName);
    if (!fromId) continue;

    const handoffEls = arrayPropElements(cfg, "handoffs");
    if (handoffEls) {
      for (const el of handoffEls) {
        const toId = resolveHandoffTarget(el);
        if (toId) b.pushEdge(fromId, toId);
      }
    }

    // Optional agents-as-tools: `tools: [child.asTool({...})]`. A resolvable
    // declared-agent receiver becomes a directed edge parent->child; non-agent
    // tools and opaque receivers are skipped.
    const toolEls = arrayPropElements(cfg, "tools");
    if (toolEls) {
      for (const el of toolEls) {
        const toId = resolveAsToolReceiver(el);
        if (toId) b.pushEdge(fromId, toId);
      }
    }
  }

  // Entry = zero-in-degree agent, computed from the SURVIVING edges. Exactly
  // one root -> entry; >1 -> ambiguous (entry_node_id ""); 0 roots (pure cycle)
  // -> fall back to the first declared agent.
  const inDeg = new Map();
  for (const id of b.nodes.keys()) inDeg.set(id, 0);
  for (const e of b.edges) inDeg.set(e.to, (inDeg.get(e.to) || 0) + 1);
  const roots = [];
  for (const id of b._order) if ((inDeg.get(id) || 0) === 0) roots.push(id);

  const nodes = Array.from(b.nodes.values());
  let entry = "";
  let entryAmbiguous = false;
  if (roots.length === 1) {
    entry = roots[0];
  } else if (roots.length > 1) {
    entryAmbiguous = true; // multiple plausible roots
  } else {
    // No root (every agent is a handoff target — a fully cyclic graph). Anchor
    // on the first declared agent so the graph still has an entry.
    entry = b._order[0] || (nodes[0] && nodes[0].id) || "";
  }

  const result = {
    nodes,
    edges: b.edges,
    entry_node_id: entryAmbiguous ? "" : entry,
    metadata: emptyMeta(filePath),
  };
  if (entryAmbiguous) {
    // Top-level signal so decodeShimGraph propagates EntryAmbiguous and
    // reachability skips the graph rather than false-flagging non-chosen roots.
    result.entry_ambiguous = true;
  }
  return result;
}

function emptyMeta(filePath) {
  return {
    source_format: "openai-agents",
    source_file: filePath || "",
    openai_agents_version: "unknown",
    extraction: "ast",
    // Handoff routing decisions are made at runtime by the model; edges are an
    // over-approximated fan-out (every declared handoff target is reachable).
    conditional_edge_reason: "over_approximated_dynamic",
  };
}

// ----- JSON-RPC plumbing ------------------------------------------------------
function emit(payload) {
  process.stdout.write(JSON.stringify(payload) + "\n");
}

function handleParseContent(params) {
  const content = params && params.content;
  if (content == null) throw new Error("parse_content: 'content' is required");
  const filename = (params && params.filename) || "<inline.ts>";
  return extract(content, filename);
}

function handleParseFile(params) {
  const p = params && params.path;
  if (!p) throw new Error("parse_file: 'path' is required");
  const content = fs.readFileSync(p, "utf8");
  return extract(content, p);
}

function handle(req) {
  const method = req.method;
  try {
    let result;
    switch (method) {
      case "health_check":
        result = { status: "ok", openai_agents_version: "unknown" };
        break;
      case "parse_file":
        result = handleParseFile(req.params || {});
        break;
      case "parse_content":
        result = handleParseContent(req.params || {});
        break;
      case "shutdown":
        emit({ id: req.id, result: { ok: true } });
        process.exit(0);
        return;
      default:
        emit({
          id: req.id,
          error: { code: -32601, message: `unknown method: ${method}` },
        });
        return;
    }
    emit({ id: req.id, result });
  } catch (err) {
    const msg = err && err.message ? err.message : String(err);
    emit({ id: req.id, error: { code: -32000, message: msg } });
  }
}

function main() {
  const rl = readline.createInterface({ input: process.stdin });
  rl.on("line", (line) => {
    const trimmed = line.trim();
    if (!trimmed) return;
    let req;
    try {
      req = JSON.parse(trimmed);
    } catch (e) {
      process.stderr.write(`openai-agents shim: bad request line: ${e}\n`);
      return;
    }
    handle(req);
  });
  rl.on("close", () => process.exit(0));
}

main();
