// Long-lived JSON-RPC worker that exports LangGraph.js StateGraph definitions
// into Shingan's WorkflowGraph JSON format, via the TypeScript Compiler API
// (AST-primary, ADR-015).
//
// Protocol (identical to the Python shims, see export_langgraph_server.py)
// =======================================================================
// Newline-delimited JSON on stdin/stdout. One request per line on stdin, one
// response per line on stdout. ALL diagnostics / library chatter go to stderr;
// stdout MUST contain only response frames or Call() framing breaks.
//
// Request:  {"id": <int|str>, "method": "<name>", "params": {...}}
// Success:  {"id": <id>, "result": <any>}
// Error:    {"id": <id>, "error": {"code": <int>, "message": "<text>"}}
//
// Methods
// -------
// - health_check   -> {"status":"ok","langgraph_version":"unknown"}
//                     The shim is AST-only; it never imports @langchain/langgraph,
//                     so health is "ok" whenever node + the bundled TS parser load.
// - parse_file     -> {"path": <abs path>} -> WorkflowGraph JSON
// - parse_content  -> {"content": <ts/js source>, "filename": <hint>} -> WorkflowGraph JSON
// - shutdown       -> {"ok": true} then process exits.
//
// On parse failure a structured JSON-RPC error is returned (graceful
// degradation); the worker never crashes.
//
// Build
// =====
// This is the *source*. The committed, runnable shim is the self-contained
// bundle `export_langgraphjs_server.mjs` (the TypeScript Compiler API vendored
// in via esbuild) which the Go embed/`//go:embed shims/*.mjs` path ships. The
// bundle is what runs — there is no node_modules at runtime. Rebuild with:
//
//   cd infrastructure/parser/shims
//   npm install            # installs typescript + esbuild (devDeps)
//   npm run build          # writes export_langgraphjs_server.mjs
//
// The esbuild `--banner` re-creates require/__filename/__dirname so the ESM
// bundle can satisfy TypeScript's internal `require("fs")` / `__filename`
// usage without a node_modules dir present (see package.json `build` script).

import * as ts from "typescript";
import * as fs from "fs";
import * as readline from "readline";

// LangGraph.js sentinels for START / END. The string forms match the runtime
// constants exported by @langchain/langgraph; the identifier forms (START/END)
// are what user code references after `import { START, END } from ...`.
const LG_START = "__start__";
const LG_END = "__end__";
const START_IDENTS = new Set(["START", "__start__"]);
const END_IDENTS = new Set(["END", "__end__"]);

// ----- node-id normalisation (mirrors the Python shim's _node_id) ----------
function nodeId(name) {
  const s = String(name);
  if (s === LG_START || s === LG_END) return s;
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

// Resolve a START/END sentinel from an AST argument node. Returns "__start__",
// "__end__", or null when the argument is not a recognised sentinel.
function sentinelOf(arg) {
  if (!arg) return null;
  // String literal: "__start__" / "__end__" / START / end keys etc.
  if (ts.isStringLiteralLike(arg)) {
    const v = arg.text;
    if (v === LG_START) return LG_START;
    if (v === LG_END) return LG_END;
    return null;
  }
  // Identifier: START / END imported from @langchain/langgraph.
  if (ts.isIdentifier(arg)) {
    if (START_IDENTS.has(arg.text)) return LG_START;
    if (END_IDENTS.has(arg.text)) return LG_END;
    return null;
  }
  // Property access: Foo.START / something.END (best-effort).
  if (ts.isPropertyAccessExpression(arg)) {
    const n = arg.name.text;
    if (START_IDENTS.has(n)) return LG_START;
    if (END_IDENTS.has(n)) return LG_END;
    return null;
  }
  return null;
}

// Extract a plain string from a string-literal AST node, else null.
function stringOf(arg) {
  if (arg && ts.isStringLiteralLike(arg)) return arg.text;
  return null;
}

// Heuristic NodeType for a node handler argument. PoC default: "llm". If the
// handler is a reference whose name hints at a tool, classify "tool". We keep
// this conservative so agent/tools stay non-loop (cycle_detection branch).
function nodeTypeForHandler(arg) {
  let name = "";
  if (!arg) return "llm";
  if (ts.isIdentifier(arg)) name = arg.text;
  else if (ts.isPropertyAccessExpression(arg)) name = arg.name.text;
  else if (
    (ts.isArrowFunction(arg) || ts.isFunctionExpression(arg)) &&
    arg.name
  )
    name = arg.name.text;
  const lower = name.toLowerCase();
  if (/(tool|retriev|search|fetch|browser)/.test(lower)) return "tool";
  return "llm";
}

// ----- per-graph accumulator -----------------------------------------------
// We track one builder per `new StateGraph(...)` assignment, keyed by the
// variable name it was assigned to, so chained calls route to the right graph.
class GraphBuilder {
  constructor(varName) {
    this.varName = varName;
    this.nodes = new Map(); // id -> {id,name,type,config,pos,has_exit_branch}
    this.edges = []; // {from,to,condition}
    this.entry = "";
    this.compiled = false;
  }

  ensureNode(id, name, type, pos) {
    if (this.nodes.has(id)) return this.nodes.get(id);
    const n = {
      id,
      name: name,
      type: type || "llm",
      config: {},
      pos: pos || { file: "", line: 0, col: 0 },
      has_exit_branch: false,
    };
    this.nodes.set(id, n);
    return n;
  }

  markExit(id) {
    const n = this.nodes.get(id);
    if (n) {
      n.has_exit_branch = true;
      n.config = n.config || {};
      n.config.has_end_branch = true;
    }
  }

  recordEntry(id) {
    if (!this.entry) this.entry = id;
  }

  pushEdge(src, dst, condition) {
    const srcSent = src === LG_START || src === LG_END;
    const dstSent = dst === LG_START || dst === LG_END;
    // START -> user node: set entry, drop edge.
    if (src === LG_START) {
      this.recordEntry(dst);
      return;
    }
    // user node -> END: drop edge, mark exit on source.
    if (dst === LG_END) {
      this.markExit(src);
      return;
    }
    // Defensive: sentinel on the wrong side.
    if (srcSent || dstSent) return;
    const e = { from: src, to: dst };
    if (condition) e.condition = condition;
    this.edges.push(e);
  }
}

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

// Walk a `new StateGraph(...)....chain` expression statement, resolving the
// root variable name when assigned. Returns the variable name or null.
function rootVarName(decl) {
  // const x = new StateGraph(...) ...
  if (ts.isVariableDeclaration(decl) && decl.name && ts.isIdentifier(decl.name))
    return decl.name.text;
  return null;
}

// Determine whether a call expression's receiver chain originates from a
// `new StateGraph(...)`. Returns the StateGraph NewExpression node or null.
function stateGraphRootOf(expr) {
  let cur = expr;
  while (cur) {
    if (ts.isNewExpression(cur)) {
      const c = cur.expression;
      if (
        (ts.isIdentifier(c) && c.text === "StateGraph") ||
        (ts.isPropertyAccessExpression(c) && c.name.text === "StateGraph")
      ) {
        return cur;
      }
      return null;
    }
    if (ts.isCallExpression(cur) && ts.isPropertyAccessExpression(cur.expression)) {
      cur = cur.expression.expression;
      continue;
    }
    if (ts.isPropertyAccessExpression(cur)) {
      cur = cur.expression;
      continue;
    }
    return null;
  }
  return null;
}

// Collect router-function destinations so a conditional whose pathMap is
// omitted still surfaces END exits. We harvest the END/START sentinels and
// string-literal returns from a router function body/annotation.
function collectRouterReturns(fnNode) {
  const dests = new Set();
  if (!fnNode) return dests;
  const visit = (n) => {
    if (ts.isReturnStatement(n) && n.expression) {
      const e = n.expression;
      const sent = sentinelOf(e);
      if (sent) dests.add(sent);
      else {
        const s = stringOf(e);
        if (s) dests.add(s);
      }
    }
    ts.forEachChild(n, visit);
  };
  if (fnNode.body) visit(fnNode.body);
  return dests;
}

// ----- the core extractor ---------------------------------------------------
function extract(content, filePath) {
  const sourceFile = ts.createSourceFile(
    filePath || "<inline.ts>",
    content,
    ts.ScriptTarget.Latest,
    /*setParentNodes*/ true,
    // Pick TS vs TSX/JS by extension; default to TS which also parses JS.
    (filePath && /\.tsx$/.test(filePath)) ? ts.ScriptKind.TSX : ts.ScriptKind.TS
  );

  const builders = new Map(); // varName -> GraphBuilder
  // Map a local function/identifier name -> its declaration node, so a router
  // passed by reference (addConditionalEdges("a", shouldContinue, map)) can be
  // resolved for END-exit detection.
  const fnDecls = new Map();

  // Pre-pass: collect function declarations and arrow/function var bindings.
  const collectFns = (node) => {
    if (ts.isFunctionDeclaration(node) && node.name) {
      fnDecls.set(node.name.text, node);
    }
    if (ts.isVariableDeclaration(node) && node.name && ts.isIdentifier(node.name) && node.initializer) {
      if (
        ts.isArrowFunction(node.initializer) ||
        ts.isFunctionExpression(node.initializer)
      ) {
        fnDecls.set(node.name.text, node.initializer);
      }
    }
    ts.forEachChild(node, collectFns);
  };
  collectFns(sourceFile);

  // Resolve the builder var for a call chain. We register the var on first
  // sight of `new StateGraph(...)` assigned to a variable.
  function builderFor(varName) {
    if (!varName) return null;
    let b = builders.get(varName);
    if (!b) {
      b = new GraphBuilder(varName);
      builders.set(varName, b);
    }
    return b;
  }

  // Find the variable name that a method-chain receiver ultimately resolves to.
  // For `graph.addNode(...)` returns "graph"; for chained
  // `new StateGraph(...).addNode(...).addEdge(...)` returns null (anonymous).
  function receiverVar(expr) {
    let cur = expr;
    while (cur) {
      if (ts.isIdentifier(cur)) return cur.text;
      if (ts.isCallExpression(cur) && ts.isPropertyAccessExpression(cur.expression)) {
        cur = cur.expression.expression;
        continue;
      }
      if (ts.isPropertyAccessExpression(cur)) {
        cur = cur.expression;
        continue;
      }
      if (ts.isNewExpression(cur)) return null; // anonymous chain
      return null;
    }
    return null;
  }

  // Register `const x = new StateGraph(...)` (possibly chained).
  const registerBuilders = (node) => {
    if (ts.isVariableDeclaration(node) && node.initializer) {
      const sg = stateGraphRootOf(node.initializer);
      if (sg) {
        const vn = rootVarName(node);
        if (vn) builderFor(vn);
      }
    }
    ts.forEachChild(node, registerBuilders);
  };
  registerBuilders(sourceFile);

  // For anonymous chains `new StateGraph(...).addNode(...)...` assigned or not,
  // we use a synthetic single builder keyed by "<anon>".
  function builderForExpr(callExpr) {
    // callExpr.expression is a PropertyAccess: <recv>.method
    const pae = callExpr.expression;
    if (!ts.isPropertyAccessExpression(pae)) return null;
    const recv = pae.expression;
    const sg = stateGraphRootOf(recv);
    if (!sg && !ts.isIdentifier(receiverHead(recv))) {
      // Not a StateGraph chain at all.
    }
    const vn = receiverVar(recv);
    if (vn && builders.has(vn)) return builders.get(vn);
    if (sg) {
      // Anonymous chain rooted at `new StateGraph(...)`.
      return builderFor("<anon>");
    }
    if (vn && builders.size === 0) {
      // Tolerate a builder var we didn't catch in registerBuilders.
      return builderFor(vn);
    }
    return vn ? builders.get(vn) || null : null;
  }

  function receiverHead(expr) {
    let cur = expr;
    while (cur && ts.isPropertyAccessExpression(cur)) cur = cur.expression;
    if (cur && ts.isCallExpression(cur)) return cur.expression;
    return cur;
  }

  // Process builder method calls in two phases so that fully-chained graphs
  // (`new StateGraph().addNode("a").addEdge("a", END)`) work regardless of AST
  // visitation order. A chained expression is visited outermost-call-first by
  // forEachChild, so an `addEdge(..., END)` would run its markExit("a") before
  // the inner `addNode("a")` ever registers the node. Splitting into:
  //   phase "nodes" — addNode / setEntryPoint (populate the node maps)
  //   phase "edges" — addEdge / addConditionalEdges (depend on nodes existing)
  // makes pushEdge/markExit order-independent.
  const handleCall = (node, phase) => {
    if (ts.isCallExpression(node) && ts.isPropertyAccessExpression(node.expression)) {
      const method = node.expression.name.text;
      const recv = node.expression.expression;
      // Only handle when the chain originates from a StateGraph.
      const isSGChain =
        stateGraphRootOf(recv) !== null ||
        (() => {
          const vn = receiverVar(recv);
          return vn && builders.has(vn);
        })();
      if (isSGChain) {
        const b = builderForExpr(node);
        if (b) {
          const args = node.arguments;
          if (phase === "nodes" && method === "addNode") {
            const name = stringOf(args[0]);
            if (name) {
              const id = nodeId(name);
              b.ensureNode(
                id,
                name,
                nodeTypeForHandler(args[1]),
                posOf(sourceFile, node, filePath)
              );
            }
          } else if (phase === "edges" && method === "addEdge") {
            // addEdge(from, to). from/to may be sentinel or string, or an
            // array of sources (fan-in) for the first arg.
            const tos = sentinelOf(args[1]) || stringOf(args[1]);
            const froms = [];
            if (args[0] && ts.isArrayLiteralExpression(args[0])) {
              for (const el of args[0].elements) {
                const f = sentinelOf(el) || stringOf(el);
                if (f) froms.push(f);
              }
            } else {
              const f = sentinelOf(args[0]) || stringOf(args[0]);
              if (f) froms.push(f);
            }
            if (tos) {
              for (const f of froms) {
                b.pushEdge(
                  f === LG_START || f === LG_END ? f : nodeId(f),
                  tos === LG_START || tos === LG_END ? tos : nodeId(tos),
                  ""
                );
              }
            }
          } else if (phase === "edges" && method === "addConditionalEdges") {
            handleConditional(b, args);
          } else if (phase === "nodes" && method === "setEntryPoint") {
            const name = stringOf(args[0]) || sentinelOf(args[0]);
            if (name && name !== LG_START && name !== LG_END)
              b.recordEntry(nodeId(name));
          }
        }
      }
    }
    ts.forEachChild(node, (child) => handleCall(child, phase));
  };

  function handleConditional(b, args) {
    const src = stringOf(args[0]) || sentinelOf(args[0]);
    if (!src || src === LG_START || src === LG_END) return;
    const srcId = nodeId(src);
    // Router is args[1]; pathMap is args[2] (object literal or array).
    const routerArg = args[1];
    const pathMap = args[2];
    let sawEnd = false;
    let emitted = false;

    if (pathMap && ts.isObjectLiteralExpression(pathMap)) {
      for (const prop of pathMap.properties) {
        if (!ts.isPropertyAssignment(prop)) continue;
        // key is the router's return value; value is the destination.
        let key = "";
        if (ts.isStringLiteralLike(prop.name)) key = prop.name.text;
        else if (ts.isIdentifier(prop.name)) key = prop.name.text;
        else if (ts.isComputedPropertyName(prop.name)) {
          const s = sentinelOf(prop.name.expression) || stringOf(prop.name.expression);
          if (s) key = s;
        }
        const destSent = sentinelOf(prop.initializer);
        const destStr = stringOf(prop.initializer);
        const dest = destSent || destStr;
        if (!dest) continue;
        if (dest === LG_END) {
          sawEnd = true;
          // Drop the edge; mark exit below.
          continue;
        }
        if (dest === LG_START) continue;
        b.pushEdge(srcId, nodeId(dest), key || "");
        emitted = true;
      }
    } else if (pathMap && ts.isArrayLiteralExpression(pathMap)) {
      // Array path map: each element is a destination keyed by itself.
      for (const el of pathMap.elements) {
        const destSent = sentinelOf(el);
        const destStr = stringOf(el);
        const dest = destSent || destStr;
        if (!dest) continue;
        if (dest === LG_END) {
          sawEnd = true;
          continue;
        }
        if (dest === LG_START) continue;
        b.pushEdge(srcId, nodeId(dest), dest);
        emitted = true;
      }
    }

    // Inspect the router function (if resolvable) for END returns. This covers
    // the no-pathMap form addConditionalEdges("a", shouldContinue) and also
    // catches END exits the pathMap omitted. Approximation: we only resolve
    // routers passed by name reference or inline arrow/function.
    let routerFn = null;
    if (routerArg) {
      if (ts.isIdentifier(routerArg)) routerFn = fnDecls.get(routerArg.text) || null;
      else if (ts.isArrowFunction(routerArg) || ts.isFunctionExpression(routerArg))
        routerFn = routerArg;
    }
    if (routerFn) {
      const dests = collectRouterReturns(routerFn);
      for (const d of dests) {
        if (d === LG_END) sawEnd = true;
      }
      // When there is no pathMap, materialise string-literal destinations the
      // router returns (best-effort over-approximation).
      if (!emitted && !pathMap) {
        for (const d of dests) {
          if (d === LG_END) {
            sawEnd = true;
          } else if (d === LG_START) {
            // skip
          } else {
            b.pushEdge(srcId, nodeId(d), d);
          }
        }
      }
    }

    if (sawEnd) b.markExit(srcId);
  }

  // Phase 1: register all nodes / explicit entry points. Phase 2: edges +
  // conditionals (which rely on nodes already existing for markExit/entry).
  handleCall(sourceFile, "nodes");
  handleCall(sourceFile, "edges");

  // Pick the "main" graph: prefer a builder with the most nodes; ties broken
  // by first-registered. Mirrors the Python AST visitor's largest-graph
  // fallback. Most PoC files declare exactly one StateGraph.
  let best = null;
  for (const b of builders.values()) {
    if (!best || b.nodes.size > best.nodes.size) best = b;
  }
  if (!best) {
    return {
      nodes: [],
      edges: [],
      entry_node_id: "",
      metadata: emptyMeta(filePath),
    };
  }

  // Entry fallback: first node when neither START edge nor setEntryPoint set.
  let entry = best.entry;
  const nodes = Array.from(best.nodes.values());
  if (!entry && nodes.length) entry = nodes[0].id;

  return {
    nodes,
    edges: best.edges,
    entry_node_id: entry,
    metadata: emptyMeta(filePath),
  };
}

function emptyMeta(filePath) {
  return {
    source_format: "langgraph-js",
    source_file: filePath || "",
    langgraph_version: "unknown",
    conditional_edge_reason: "over_approximated_dynamic",
  };
}

// ----- JSON-RPC plumbing ----------------------------------------------------
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
        result = { status: "ok", langgraph_version: "unknown" };
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
      // Cannot recover request id; log to stderr and skip.
      process.stderr.write(`langgraphjs shim: bad request line: ${e}\n`);
      return;
    }
    handle(req);
  });
  rl.on("close", () => process.exit(0));
}

main();
