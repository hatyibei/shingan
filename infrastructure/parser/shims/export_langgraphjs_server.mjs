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
// Synthetic entry node id used to model a START fan-out (START -> A, START -> B,
// ...). LangGraph.js runs every START successor in parallel; the AST-only shim
// has no runtime to resolve that, so when START has >=2 successors we
// materialise `__start__` as a Control entry node with one edge to each
// successor, letting reachability flow to all parallel entries (rather than the
// pre-fix behaviour of keeping a single one and false-flagging the rest).
const LG_START_NODE = "__start__";
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

// Extract the STATIC text of a string-literal or template-literal AST node, for
// prompt scanning. For a template literal we keep the literal spans and DROP the
// `${…}` interpolations (the static text is what carries a pasted secret). A
// plain/no-substitution template (`\`…\``) returns its single cooked text.
// Returns the text, or null when `arg` is not a string-like node.
function staticTextOf(arg) {
  if (!arg) return null;
  if (ts.isStringLiteralLike(arg)) return arg.text;
  // No-substitution template literal: `\`hello\``.
  if (ts.isNoSubstitutionTemplateLiteral(arg)) return arg.text;
  // Template expression: `\`head ${x} tail\`` -> concat the static spans only.
  if (ts.isTemplateExpression(arg)) {
    let out = arg.head.text;
    for (const span of arg.templateSpans) out += span.literal.text;
    return out;
  }
  // String concatenation of literals: `"a" + "b" + …` (a common multi-line
  // prompt factoring). Recurse both operands of a `+`; a non-resolvable operand
  // (a runtime variable) contributes empty text rather than aborting, so the
  // STATIC text around it is still captured (where a pasted secret lives).
  if (
    ts.isBinaryExpression(arg) &&
    arg.operatorToken.kind === ts.SyntaxKind.PlusToken
  ) {
    const l = staticTextOf(arg.left);
    const r = staticTextOf(arg.right);
    if (l == null && r == null) return null;
    return (l || "") + (r || "");
  }
  // Parenthesised expression — unwrap.
  if (ts.isParenthesizedExpression(arg)) return staticTextOf(arg.expression);
  return null;
}

// ----- zod → JSON-schema conversion (security: unbounded_tool_arg) ----------
// The Go rule `unbounded_tool_arg` scans a Tool node's `config.args_schema` as a
// JSON-schema map and flags a string field with no `maxLength` or an array with
// no `maxItems`. langgraph-js tools declare their args as a zod schema on the
// `tool(fn, { schema: z.object({…}) })` DEFINITION. We convert the common zod
// forms to the JSON-schema shape the rule walks:
//   z.string()            -> {"type":"string"}
//   z.string().max(4000)  -> {"type":"string","maxLength":4000}
//   z.number()/.int()     -> {"type":"number"}  (z.number().max(n) -> "maximum")
//   z.array(x)            -> {"type":"array","items":<x>}  (.max(n)->"maxItems")
//   z.object({a:…,b:…})   -> {"type":"object","properties":{a:…,b:…}}
// Exotic forms (unions, .refine, .transform, custom) degrade to null and are
// omitted — never fabricated (ADR-015). `.optional()`/`.nullable()`/`.describe()`/
// `.default()` are transparent wrappers: we unwrap to the inner type and ignore
// the modifier (presence/optionality doesn't affect the unbounded-arg scan).

// Read a numeric literal from a `.max(n)` / `.length(n)` argument, else null.
function numericArg(arg) {
  if (!arg) return null;
  if (ts.isNumericLiteral(arg)) {
    const v = Number(arg.text);
    return Number.isFinite(v) ? v : null;
  }
  // `-1` etc. (PrefixUnaryExpression) — not a meaningful bound; ignore.
  return null;
}

// Walk a zod expression's fluent chain, collecting the base `z.<type>(...)` call
// and the trailing `.method(args)` modifiers (max/min/length/optional/…). Returns
// { base, mods: [{name, args}] } or null when the head is not a `z.<type>` call.
function zodChain(expr) {
  const mods = [];
  let cur = expr;
  // Unwrap trailing `.method(...)` calls until we reach the base `z.<type>(...)`.
  while (cur && ts.isCallExpression(cur) && ts.isPropertyAccessExpression(cur.expression)) {
    const methodName = cur.expression.name.text;
    const recv = cur.expression.expression;
    // Base case: `z.string(...)` / `z.object(...)` — the receiver of this call's
    // property access is the `z` identifier (or aliased import).
    if (ts.isIdentifier(recv) && (recv.text === "z" || recv.text === "zod")) {
      mods.reverse();
      return { type: methodName, baseCall: cur, mods };
    }
    // Modifier: `.max(4000)` / `.optional()` — record and descend.
    mods.push({ name: methodName, args: cur.arguments });
    cur = recv;
  }
  return null;
}

// Convert a zod schema AST expression to a JSON-schema map, or null when the
// form is not one of the handled common cases.
function zodToJsonSchema(expr, depth) {
  depth = depth || 0;
  if (!expr || depth > 8) return null; // guard pathological nesting
  const chain = zodChain(expr);
  if (!chain) return null;
  const { type, baseCall, mods } = chain;

  // Find a modifier by name (last one wins, already in call order).
  const modVal = (name) => {
    let v = null;
    for (const m of mods) {
      if (m.name === name && m.args && m.args.length) {
        const n = numericArg(m.args[0]);
        if (n != null) v = n;
      }
    }
    return v;
  };

  switch (type) {
    case "string":
    case "enum": {
      const out = { type: "string" };
      // `.max(n)` or `.length(n)` both impose an upper bound.
      const mx = modVal("max");
      const len = modVal("length");
      let bound = mx != null ? mx : len;
      // A `z.enum([...])` is a FINITE set — bounded by its longest literal. Derive
      // a maxLength from the enum members so unbounded_tool_arg does NOT flag a
      // closed enum as an unbounded string (codex #52). z.enum literals are always
      // string literals.
      if (
        bound == null &&
        type === "enum" &&
        baseCall.arguments &&
        baseCall.arguments[0] &&
        ts.isArrayLiteralExpression(baseCall.arguments[0])
      ) {
        let longest = 0;
        for (const el of baseCall.arguments[0].elements) {
          if (ts.isStringLiteralLike(el)) longest = Math.max(longest, el.text.length);
        }
        if (longest > 0) bound = longest;
      }
      if (bound != null) out.maxLength = bound;
      return out;
    }
    case "number":
    case "bigint": {
      const out = { type: "number" };
      const mx = modVal("max");
      if (mx != null) out.maximum = mx;
      return out;
    }
    case "boolean":
      return { type: "boolean" };
    case "array": {
      const out = { type: "array" };
      const inner = baseCall.arguments && baseCall.arguments[0];
      const itemSchema = inner ? zodToJsonSchema(inner, depth + 1) : null;
      if (itemSchema) out.items = itemSchema;
      const mx = modVal("max");
      const len = modVal("length");
      const bound = mx != null ? mx : len;
      if (bound != null) out.maxItems = bound;
      return out;
    }
    case "object": {
      const out = { type: "object", properties: {} };
      const inner = baseCall.arguments && baseCall.arguments[0];
      if (inner && ts.isObjectLiteralExpression(inner)) {
        for (const prop of inner.properties) {
          if (!ts.isPropertyAssignment(prop)) continue;
          let key = "";
          if (ts.isIdentifier(prop.name)) key = prop.name.text;
          else if (ts.isStringLiteralLike(prop.name)) key = prop.name.text;
          if (!key) continue;
          const fieldSchema = zodToJsonSchema(prop.initializer, depth + 1);
          if (fieldSchema) out.properties[key] = fieldSchema;
        }
      }
      return out;
    }
    default:
      // optional/nullable/default/describe applied directly on a `z.<exotic>` we
      // don't model, or an unhandled base (union, record, …) -> omit gracefully.
      return null;
  }
}

// Constructor name of a `new Foo(...)` / `new ns.Foo(...)` expression, else "".
function ctorName(newExpr) {
  if (!newExpr || !ts.isNewExpression(newExpr)) return "";
  const c = newExpr.expression;
  if (ts.isIdentifier(c)) return c.text;
  if (ts.isPropertyAccessExpression(c)) return c.name.text;
  return "";
}

// Name-based NodeType hint, mirroring the Python `_node_type_for` heuristic
// (handler identifier/attribute name → tool/llm). Conservative default "llm".
// This is the *name* signal; body inspection (classifyHandler in extract)
// layers on top of it. Returns "tool", "llm", or "" (no name signal).
function nodeTypeForName(arg) {
  let name = "";
  if (!arg) return "";
  if (ts.isIdentifier(arg)) name = arg.text;
  else if (ts.isPropertyAccessExpression(arg)) name = arg.name.text;
  else if (
    (ts.isArrowFunction(arg) || ts.isFunctionExpression(arg)) &&
    arg.name
  )
    name = arg.name.text;
  const lower = name.toLowerCase();
  if (/(tool|retriev|search|fetch|browser)/.test(lower)) return "tool";
  return "";
}

// Chat-model / LLM construct identifiers. A handler body that constructs or
// invokes one of these is an LLM node. The `.bindTools`/`.invoke`/`.stream`
// method names and the `llm`/`model` identifier hints are matched separately
// (callee/identifier inspection) since they aren't constructors.
const LLM_CTORS = new Set([
  "ChatOpenAI",
  "ChatAnthropic",
  "ChatGoogleGenerativeAI",
  "ChatVertexAI",
  "ChatBedrock",
  "ChatMistralAI",
  "ChatCohere",
  "ChatFireworks",
  "ChatGroq",
  "ChatOllama",
  "AzureChatOpenAI",
]);
// `.bindTools(...)` is unambiguously a chat-model method. `.invoke`/`.stream`
// are generic Runnable methods, so they only signal an LLM when their receiver
// is model-like (a ChatX construct or a `model`/`llm` identifier).
const LLM_BIND_METHODS = new Set(["bindTools", "bind_tools"]);
const LLM_RUN_METHODS = new Set(["invoke", "stream"]);
const LLM_IDENTS = new Set(["llm", "model", "chatModel", "chat_model"]);
// Tool-execution constructs. A `new ToolNode(...)` handler, or a body that
// references a tools array / calls a tool-execution API, is a tool node.
const TOOL_CTORS = new Set(["ToolNode"]);
const TOOL_IDENTS = new Set(["tools", "toolNode", "toolExecutor", "toolnode"]);

// ----- prompt-string extraction (security: secret_in_prompt_template) -------
// The Go rule `secret_in_prompt_template` scans an LLM node's
// `config.system_prompt` for hardcoded credentials. The AST-only shim must
// therefore lift the *static prompt text* out of an LLM handler body and stash
// it on the node config. We extract text from genuine prompt POSITIONS only
// (NOT every string literal — capturing `"gpt-4o"` from `new ChatOpenAI({model})`
// would be noise and could newly classify a trivial node as a prompt-injection
// sink). Recognised positions:
//   * `new SystemMessage("…")` / `SystemMessage("…")`     (message ctor text arg)
//   * `{ role: "system"|"user"|…, content: "…" }`         (message object content)
//   * `llm.invoke("…")` / `model.invoke([...])`           (string arg to a
//                                                          model-like .invoke/.stream)
//   * `const systemPrompt = "…"` / `const prompt = "…"`   (prompt-named binding)
// Degrade gracefully (ADR-015): extract obvious prompt strings, omit anything
// that cannot be statically resolved. Never fabricate.
//
// LangChain message constructors whose first string-like argument is prompt
// text. A pasted secret in ANY message string is a leak, so human/tool message
// ctors are included alongside SystemMessage.
const PROMPT_MESSAGE_CTORS = new Set([
  "SystemMessage",
  "HumanMessage",
  "AIMessage",
  "ToolMessage",
  "FunctionMessage",
  "ChatMessage",
]);
// Roles whose `{ role, content }` object literal carries prompt text worth
// scanning. "system" is the highest-value case; user/assistant content is
// included since a hardcoded secret in any message string still leaks.
const PROMPT_MESSAGE_ROLES = new Set([
  "system",
  "user",
  "human",
  "assistant",
  "ai",
  "developer",
]);
// `const <name> = "…"` bindings whose NAME looks prompt-like are lifted even
// when not passed directly to an invoke (a very common factoring). Matched
// loosely; the Go secret scan tolerates non-prompt noise (it only fires on a
// credential pattern) so a slightly over-broad name match is safe.
const PROMPT_BINDING_RE = /(prompt|system|instruction|template|persona|preamble)/i;

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
    this.handlers = new Map(); // node id -> handler AST node (for Command goto)
    this.nodeEnds = new Map(); // node id -> ends:[...] static-routing dest list
    // Every node id that START routes to, collected from BOTH plain
    // `addEdge(START, X)` edges AND `addConditionalEdges(START, router, [...])`
    // pathMaps/router dests. We collect them all (instead of keeping the first
    // as `entry` and dropping the rest) so a START fan-out can be modelled as a
    // synthetic `__start__` Control node that reaches every parallel entry.
    // Insertion order is preserved so the single-successor entry stays stable.
    this.startSuccessors = new Set();
    // Node ids contributed by each distinct `new StateGraph(...)` root in this
    // (possibly varname-merged) builder. Two roots covering DISJOINT node sets
    // mean the file declared several independent graphs that happened to reuse
    // the same variable name (`const workflow = new StateGraph(...)` x3 — the
    // Magic-Resume shape); we surface that as an ambiguous entry so reachability
    // skips rather than false-flagging graphs 2..N. Keyed by the StateGraph
    // NewExpression node identity (stable across a fluent chain).
    this.rootNodeSets = new Map(); // NewExpression node -> Set<node id>
  }

  // Record that `id` is a START successor (an entry candidate). Kept separate
  // from `recordEntry` so a >=2-way fan-out is not lossily collapsed to one.
  addStartSuccessor(id) {
    if (id && id !== LG_START && id !== LG_END) this.startSuccessors.add(id);
  }

  // Attribute a freshly-registered node id to the StateGraph root it was added
  // through, so a disjoint-root-set (multi-graph) file can be detected.
  recordRootNode(rootNode, id) {
    if (!rootNode || !id) return;
    let set = this.rootNodeSets.get(rootNode);
    if (!set) {
      set = new Set();
      this.rootNodeSets.set(rootNode, set);
    }
    set.add(id);
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
    // START -> user node: record the successor (entry candidate), drop the edge.
    // The actual entry (single node vs synthetic `__start__` fan-out) is
    // resolved once, in the post-pass, after every START successor is known.
    if (src === LG_START) {
      this.addStartSuccessor(dst);
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

// Harvest router destinations from a return-type annotation, e.g.
//   function route(s): "tools" | typeof END
//   (s): "a" | "__end__"
// We walk the annotation type tree and collect:
//   * string-literal types ("tools", "__end__")           -> the literal text
//   * `typeof END` (TypeQueryNode over a sentinel ident)   -> the LG sentinel
//   * sentinel identifiers / property accesses             -> the LG sentinel
// Resolution is genuinely impossible for opaque/imported annotation types, in
// which case nothing is added (no regression). Mirrors the Python
// `_extract_command_dests_from_annotation` Literal[...] handling.
function collectAnnotationDests(typeNode, out) {
  if (!typeNode) return;
  // Union: `"tools" | typeof END` -> recurse each member.
  if (ts.isUnionTypeNode(typeNode)) {
    for (const t of typeNode.types) collectAnnotationDests(t, out);
    return;
  }
  if (ts.isParenthesizedTypeNode && ts.isParenthesizedTypeNode(typeNode)) {
    collectAnnotationDests(typeNode.type, out);
    return;
  }
  // Literal string type: `"tools"` / `"__end__"`.
  if (ts.isLiteralTypeNode(typeNode)) {
    const lit = typeNode.literal;
    if (lit && ts.isStringLiteralLike(lit)) {
      if (lit.text === LG_END) out.add(LG_END);
      else if (lit.text === LG_START) out.add(LG_START);
      else out.add(lit.text);
    }
    return;
  }
  // `typeof END` -> TypeQueryNode whose exprName is the sentinel identifier.
  if (ts.isTypeQueryNode(typeNode)) {
    const name = typeNode.exprName;
    let id = "";
    if (name && ts.isIdentifier(name)) id = name.text;
    else if (name && ts.isQualifiedName(name)) id = name.right.text;
    if (END_IDENTS.has(id)) out.add(LG_END);
    else if (START_IDENTS.has(id)) out.add(LG_START);
    return;
  }
  // Bare type reference to a sentinel (rare): `END` used as a type.
  if (ts.isTypeReferenceNode(typeNode)) {
    const tn = typeNode.typeName;
    let id = "";
    if (ts.isIdentifier(tn)) id = tn.text;
    else if (ts.isQualifiedName(tn)) id = tn.right.text;
    if (END_IDENTS.has(id)) out.add(LG_END);
    else if (START_IDENTS.has(id)) out.add(LG_START);
    return;
  }
}

// Collect router-function destinations so a conditional whose pathMap is
// omitted still surfaces END exits. We harvest the END/START sentinels and
// string-literal returns from a router function body AND its return-type
// annotation. Body harvest recurses every `return` (not just the tail), so a
// `return END` nested in an `if` is caught; the annotation harvest catches END
// exits a fully-opaque body would otherwise hide.
function collectRouterReturns(fnNode) {
  const dests = new Set();
  if (!fnNode) return dests;
  // Local `const/let X = …` bindings declared INSIDE this router fn, so a
  // `const next = "rewrite"; return next` resolves to THIS router's binding —
  // not a same-named binding in another router (a file-global map keeps only the
  // first declaration and would cross-wire routers — codex #51). First binding
  // per name within the fn wins.
  const localBindings = new Map();
  const collectBindings = (n) => {
    if (
      ts.isVariableDeclaration(n) &&
      n.name &&
      ts.isIdentifier(n.name) &&
      n.initializer &&
      !localBindings.has(n.name.text)
    ) {
      localBindings.set(n.name.text, n.initializer);
    }
    ts.forEachChild(n, collectBindings);
  };
  if (fnNode.body) collectBindings(fnNode.body);

  // Resolve a returned expression to its destination string(s). Recurses through
  // parentheses, ternary branches (`cond ? "a" : "b"` — BOTH arms are dests), and
  // a locally-bound identifier (`const t = "a"; return t`). Without the ternary /
  // binding recursion a path-map-less `addConditionalEdges(node, router)` whose
  // router returns a ternary dropped the targets → false unreachable on them
  // (dogfood: ternary-return routers, e.g. `s.ok ? "answer" : "rewrite"`).
  const addDest = (e, seen) => {
    if (!e) return;
    if (ts.isParenthesizedExpression(e)) return addDest(e.expression, seen);
    if (ts.isConditionalExpression(e)) {
      addDest(e.whenTrue, seen);
      addDest(e.whenFalse, seen);
      return;
    }
    const sent = sentinelOf(e);
    if (sent) { dests.add(sent); return; }
    const s = stringOf(e);
    if (s) { dests.add(s); return; }
    if (ts.isIdentifier(e) && localBindings.has(e.text)) {
      seen = seen || new Set();
      if (seen.has(e.text)) return; // guard self-referential bindings
      seen.add(e.text);
      addDest(localBindings.get(e.text), seen);
    }
  };
  const visit = (n) => {
    if (ts.isReturnStatement(n) && n.expression) addDest(n.expression);
    ts.forEachChild(n, visit);
  };
  if (fnNode.body) {
    // Block body → harvest every `return`. A concise arrow body (`(s) => EXPR`,
    // e.g. `(s) => s.ok ? "answer" : "rewrite"`) has NO ReturnStatement — the
    // body itself is the returned expression, so harvest it directly.
    if (ts.isBlock(fnNode.body)) visit(fnNode.body);
    else addDest(fnNode.body);
  }
  // Return-type annotation (gap 3a): `function route(s): "tools" | typeof END`.
  if (fnNode.type) collectAnnotationDests(fnNode.type, dests);
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
  // Method-name collision tracking: a name defined by >=2 class methods makes a
  // bare `this.method` reference ambiguous (fnDecls keeps only the first body),
  // so resolveHandlerFn refuses to resolve it rather than graft the wrong
  // class's Command gotos onto another graph (codex review #36).
  const methodNameCount = new Map();
  const ambiguousMethods = new Set();
  // Map a variable name -> its initializer expression node, so a handler like
  // `const toolNode = new ToolNode(tools)` can be resolved for node-type
  // classification even when it's passed to addNode by reference.
  const varInits = new Map();
  // Identity map from a `new StateGraph(...)` NewExpression node -> the variable
  // it was assigned to. Keying by the AST node *object* (parent nodes are set,
  // so the same root node instance is shared by the declaration initializer and
  // by every chained `.addNode(...)` receiver) lets builderForExpr resolve a
  // `const g = new StateGraph(...).addNode("a", a)` chain to `g` instead of a
  // synthetic <anon> builder — closing the fluent-chain builder split.
  const sgNodeToVar = new Map();

  // Pre-pass: collect function declarations and arrow/function var bindings.
  const collectFns = (node) => {
    if (ts.isFunctionDeclaration(node) && node.name) {
      fnDecls.set(node.name.text, node);
    }
    // Class method declarations (`approveToolCall(state) { ... }`). A handler
    // passed as `this.approveToolCall.bind(this)` resolves through fnDecls by
    // the bare method name (resolveHandlerFn unwraps the .bind + this. access).
    // A MethodDeclaration has a `.body`, so collectCommandGotos / classifyFnBody
    // work on it unchanged. Don't clobber a same-named top-level function — a
    // free `function foo` is a more direct handler target than a class method.
    if (ts.isMethodDeclaration(node) && node.name && ts.isIdentifier(node.name)) {
      const mn = node.name.text;
      const seen = (methodNameCount.get(mn) || 0) + 1;
      methodNameCount.set(mn, seen);
      if (seen >= 2) ambiguousMethods.add(mn); // same method name in >=2 classes
      // Don't clobber a same-named top-level function — a free `function foo` is
      // a more direct handler target than a class method.
      if (!fnDecls.has(mn)) {
        fnDecls.set(mn, node);
      }
    }
    if (ts.isVariableDeclaration(node) && node.name && ts.isIdentifier(node.name) && node.initializer) {
      if (
        ts.isArrowFunction(node.initializer) ||
        ts.isFunctionExpression(node.initializer)
      ) {
        fnDecls.set(node.name.text, node.initializer);
      }
      // Record the initializer of every simple `const x = <expr>` binding so a
      // handler passed to addNode by reference (e.g. `const toolNode =
      // new ToolNode(tools)`) can be classified by inspecting <expr>.
      if (!varInits.has(node.name.text)) {
        varInits.set(node.name.text, node.initializer);
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
        if (vn) {
          builderFor(vn);
          // Bind the StateGraph root NewExpression *node identity* to its
          // declared variable. builderForExpr consults this so a chained
          // `const g = new StateGraph(...).addNode("a", a)` routes "a" to g's
          // builder — not a synthetic <anon> one — keeping a chain that's then
          // continued via `g.addNode("b", b)` as a single graph.
          sgNodeToVar.set(sg, vn);
        }
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
      // A StateGraph root that was assigned to a variable resolves to that
      // variable's builder even mid-chain (`const g = new StateGraph(...)
      // .addNode("a", a)`), so the chain is not split off into <anon>.
      const owner = sgNodeToVar.get(sg);
      if (owner) return builderFor(owner);
      // Truly anonymous chain rooted at `new StateGraph(...)`.
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

  // Classify a node handler argument as "tool" or "llm" (the conservative
  // default). This is additive over the Python name heuristic: a name hint
  // (nodeTypeForName) is honoured first, then the handler's *body / construct*
  // is inspected. The handler may be:
  //   addNode("x", fnRef)          -> resolve fnRef via fnDecls / varInits
  //   addNode("x", () => {...})    -> inspect the inline body directly
  //   addNode("x", new ToolNode()) -> inspect the construction expression
  //
  // Precedence (a wrong type is worse than the safe "llm" default, so the
  // signals are deliberately ordered, not summed):
  //   1. name regex says "tool"                          -> tool
  //   2. handler IS / resolves to a `new ToolNode(...)`  -> tool
  //   3. body has a chat-model construct/invoke signal   -> llm  (wins ties:
  //      the classic agent node does `model.bindTools(tools)` — both a model
  //      and a tools reference — and must stay llm)
  //   4. body references a tools array / tool-exec API   -> tool
  //   5. otherwise                                       -> llm
  function classifyHandler(arg, seen) {
    if (!arg) return "llm";
    seen = seen || new Set();

    // Name signal first (cheap, matches Python parity).
    if (nodeTypeForName(arg) === "tool") return "tool";

    // Resolve a reference (identifier) to its declaration / initializer once.
    if (ts.isIdentifier(arg)) {
      const nm = arg.text;
      if (seen.has(nm)) return "llm";
      seen.add(nm);
      const init = varInits.get(nm);
      if (init && !ts.isArrowFunction(init) && !ts.isFunctionExpression(init)) {
        // e.g. `const toolNode = new ToolNode(tools)` — classify the value.
        const t = classifyHandler(init, seen);
        if (t === "tool") return "tool";
      }
      const fn = fnDecls.get(nm);
      if (fn) return classifyFnBody(fn);
      // Unresolvable reference: no body to inspect, fall back to llm.
      return "llm";
    }

    // `new ToolNode(...)` (or other tool constructor) handler.
    if (ts.isNewExpression(arg)) {
      const cn = ctorName(arg);
      if (cn && TOOL_CTORS.has(cn)) return "tool";
      if (cn && LLM_CTORS.has(cn)) return "llm";
      return "llm";
    }

    // Inline arrow/function handler — inspect its body.
    if (ts.isArrowFunction(arg) || ts.isFunctionExpression(arg)) {
      return classifyFnBody(arg);
    }

    return "llm";
  }

  // Is `expr` a model-like receiver for a `.invoke`/`.stream` call? True for a
  // `model`/`llm` identifier, a `new ChatX(...)` construct, or a chain rooted at
  // either (e.g. `model.bindTools(tools).invoke(...)`,
  // `new ChatOpenAI(...).bindTools(tools)`). Resolves an identifier through
  // varInits once so `const model = new ChatOpenAI(...)` counts.
  function receiverIsModelLike(expr, seen) {
    if (!expr) return false;
    if (ts.isIdentifier(expr)) {
      if (LLM_IDENTS.has(expr.text)) return true;
      seen = seen || new Set();
      if (seen.has(expr.text)) return false;
      seen.add(expr.text);
      const init = varInits.get(expr.text);
      return init ? receiverIsModelLike(init, seen) : false;
    }
    if (ts.isNewExpression(expr)) {
      const cn = ctorName(expr);
      return !!(cn && LLM_CTORS.has(cn));
    }
    // Walk through a call/property chain to its head (e.g. `.bindTools(...)`).
    if (ts.isCallExpression(expr)) return receiverIsModelLike(expr.expression, seen);
    if (ts.isPropertyAccessExpression(expr)) return receiverIsModelLike(expr.expression, seen);
    return false;
  }

  // Inspect a function body for model vs tool signals (precedence: model wins).
  function classifyFnBody(fn) {
    if (!fn || !fn.body) return "llm";
    let sawModel = false;
    let sawTool = false;
    const visit = (n) => {
      // `new ChatOpenAI(...)` / `new ToolNode(...)`.
      if (ts.isNewExpression(n)) {
        const cn = ctorName(n);
        if (cn && LLM_CTORS.has(cn)) sawModel = true;
        else if (cn && TOOL_CTORS.has(cn)) sawTool = true;
      }
      // Method calls: `model.bindTools(...)`, `llm.invoke(...)`, `.stream(...)`.
      if (
        ts.isCallExpression(n) &&
        ts.isPropertyAccessExpression(n.expression)
      ) {
        const m = n.expression.name.text;
        // `.bindTools(...)` is a chat-model method -> LLM signal outright.
        if (LLM_BIND_METHODS.has(m)) {
          sawModel = true;
        } else if (LLM_RUN_METHODS.has(m)) {
          // `.invoke`/`.stream` are generic Runnable methods; treat as an LLM
          // signal only when the receiver is model-like (a `model`/`llm`
          // identifier, or a chained ChatX/.bindTools construct).
          if (receiverIsModelLike(n.expression.expression)) sawModel = true;
        }
      }
      // Bare identifier references to a model/tools-like name.
      if (ts.isIdentifier(n)) {
        if (LLM_IDENTS.has(n.text)) sawModel = true;
        else if (TOOL_IDENTS.has(n.text)) sawTool = true;
      }
      ts.forEachChild(n, visit);
    };
    visit(fn.body);
    if (sawModel) return "llm"; // model signal wins ties (agent node case)
    if (sawTool) return "tool";
    return "llm";
  }

  // Resolve a node handler AST argument to the function whose body should be
  // scanned for `new Command({goto: ...})`. Identifier handlers resolve through
  // fnDecls; inline arrows/functions are scanned directly. Returns the function
  // node (with a `.body`) or null when unresolvable (imported handler, etc.).
  //
  // Also unwraps the common class-method handler shapes the StateGraph builder
  // accepts by reference:
  //   * `fn.bind(this)`           -> resolve the pre-`.bind` receiver
  //   * `this.method` (PropertyAccess on `this`) -> fnDecls.get("method"),
  //     which now holds the class MethodDeclaration body (see collectFns).
  //   * a bare `method` identifier -> fnDecls (function OR method).
  // A `.bind(this)` wrapping a `this.method` (the `this.approveToolCall.bind(this)`
  // wild shape) unwraps to the method body so its Command gotos are harvested.
  function resolveHandlerFn(arg, seen) {
    if (!arg) return null;
    if (ts.isArrowFunction(arg) || ts.isFunctionExpression(arg)) return arg;
    // `<expr>.bind(this)` — drop the `.bind(...)` and recurse on `<expr>`.
    if (
      ts.isCallExpression(arg) &&
      ts.isPropertyAccessExpression(arg.expression) &&
      arg.expression.name.text === "bind"
    ) {
      return resolveHandlerFn(arg.expression.expression, seen);
    }
    // `this.method` (or `obj.method`) — resolve the method name via fnDecls,
    // which collectFns populates with class MethodDeclaration bodies.
    if (ts.isPropertyAccessExpression(arg)) {
      // Multiple classes defining a same-named method → `this.method` is
      // ambiguous; omit rather than resolve to an arbitrary class's body.
      if (ambiguousMethods.has(arg.name.text)) return null;
      const fn = fnDecls.get(arg.name.text);
      if (fn) return fn;
      return null;
    }
    if (ts.isIdentifier(arg)) {
      const nm = arg.text;
      seen = seen || new Set();
      if (seen.has(nm)) return null;
      seen.add(nm);
      const fn = fnDecls.get(nm);
      if (fn) return fn;
    }
    return null;
  }

  // Resolve an expression to its STATIC prompt text when it is, or references, a
  // prompt string. Handles a string/template literal directly and a
  // prompt-named identifier resolved one hop through varInits/fnDecls
  // (`const systemPrompt = "…"; …invoke(systemPrompt)`). Returns text or null.
  function resolvePromptText(expr, seen, localBindings) {
    const lit = staticTextOf(expr);
    if (lit != null) return lit;
    if (expr && ts.isIdentifier(expr)) {
      const nm = expr.text;
      seen = seen || new Set();
      if (seen.has(nm)) return null;
      seen.add(nm);
      // Prefer a binding declared INSIDE the handler being scanned (codex #52):
      // two handlers reusing `const systemPrompt = …` must each resolve to their
      // OWN value, not the file-global first declaration (which could leak an
      // earlier node's secret into a later safe node). Fall back to a module-level
      // binding only when the name isn't local to this handler.
      const init = (localBindings && localBindings.get(nm)) || varInits.get(nm);
      if (init) return resolvePromptText(init, seen, localBindings);
    }
    return null;
  }

  // Harvest the prompt-position strings inside one LLM handler function body and
  // return them concatenated (newline-joined), or "" when none. Walks every
  // node and collects text from the recognised prompt positions only
  // (PROMPT_MESSAGE_CTORS, `{role,content}` literals, model-like `.invoke`/
  // `.stream` string args, prompt-named `const …` bindings). Order-stable and
  // de-duplicated so the same literal referenced twice is not double-counted.
  function collectPromptStrings(fn) {
    if (!fn || !fn.body) return "";
    const parts = [];
    const seenText = new Set();
    const add = (text) => {
      if (text == null || text === "") return;
      if (seenText.has(text)) return;
      seenText.add(text);
      parts.push(text);
    };
    // Bindings declared INSIDE this handler, so a prompt identifier resolves to
    // this handler's own `const systemPrompt = …` before any file-global one
    // (codex #52). First binding per name within the handler wins.
    const localBindings = new Map();
    const collectLocal = (n) => {
      if (
        ts.isVariableDeclaration(n) &&
        n.name &&
        ts.isIdentifier(n.name) &&
        n.initializer &&
        !localBindings.has(n.name.text)
      ) {
        localBindings.set(n.name.text, n.initializer);
      }
      ts.forEachChild(n, collectLocal);
    };
    collectLocal(fn.body);

    const visit = (n) => {
      // Don't descend into nested handler functions — their prompts belong to
      // whatever node uses them, not this one. (A handler rarely nests another
      // graph node, but be conservative.)
      // 1. `new SystemMessage("…")` / `SystemMessage("…")` (and HumanMessage, …).
      if (ts.isNewExpression(n) || ts.isCallExpression(n)) {
        const callee = n.expression;
        let cn = "";
        if (ts.isIdentifier(callee)) cn = callee.text;
        else if (ts.isPropertyAccessExpression(callee)) cn = callee.name.text;

        if (cn && PROMPT_MESSAGE_CTORS.has(cn)) {
          const a0 = n.arguments && n.arguments[0];
          add(resolvePromptText(a0, undefined, localBindings));
        }
        // 2. `.invoke("…")` / `.stream("…")` / `.invoke([...])` on a model-like
        //    receiver — capture string-literal args and the contents of an
        //    inline messages array.
        if (
          ts.isCallExpression(n) &&
          ts.isPropertyAccessExpression(callee) &&
          LLM_RUN_METHODS.has(callee.name.text) &&
          receiverIsModelLike(callee.expression)
        ) {
          for (const a of n.arguments || []) {
            const direct = resolvePromptText(a, undefined, localBindings);
            if (direct != null) add(direct);
            // `model.invoke([msg, msg, …])` — array elements are visited by the
            // normal walk below (message ctors / {role,content}); nothing more
            // to do here for the array itself.
          }
        }
      }
      // 3. `{ role: "system"|…, content: "…" }` object-literal message.
      if (ts.isObjectLiteralExpression(n)) {
        let role = "";
        let content = null;
        let hasContentKey = false;
        for (const prop of n.properties) {
          if (!ts.isPropertyAssignment(prop)) continue;
          let key = "";
          if (ts.isIdentifier(prop.name)) key = prop.name.text;
          else if (ts.isStringLiteralLike(prop.name)) key = prop.name.text;
          if (key === "role") {
            const r = staticTextOf(prop.initializer);
            if (r != null) role = r.toLowerCase();
          } else if (key === "content") {
            hasContentKey = true;
            content = resolvePromptText(prop.initializer, undefined, localBindings);
          }
        }
        // A `{role, content}` shaped object whose role is a known message role
        // (or which has a content key with no role, i.e. an implicit message)
        // contributes its content text.
        if (hasContentKey && content != null) {
          if (role === "" || PROMPT_MESSAGE_ROLES.has(role)) add(content);
        }
      }
      ts.forEachChild(n, visit);
    };
    visit(fn.body);

    // 4. Prompt-named `const <name> = "…"` bindings DECLARED INSIDE this handler
    //    (`const systemPrompt = "You are …"`), even if never passed to invoke —
    //    a common factoring where the secret sits in the binding. Collected via
    //    a dedicated declaration walk so it is independent of usage.
    const visitBindings = (n) => {
      if (
        ts.isVariableDeclaration(n) &&
        n.name &&
        ts.isIdentifier(n.name) &&
        n.initializer &&
        PROMPT_BINDING_RE.test(n.name.text)
      ) {
        add(staticTextOf(n.initializer));
      }
      ts.forEachChild(n, visitBindings);
    };
    visitBindings(fn.body);

    return parts.join("\n");
  }

  // For an LLM node's handler arg, lift its static prompt text (if any) so the
  // Go `secret_in_prompt_template` rule can scan `config.system_prompt`. Returns
  // "" when the handler is unresolvable or carries no recognisable prompt.
  function extractPromptForHandler(handlerArg) {
    const fn = resolveHandlerFn(handlerArg);
    if (!fn) return "";
    return collectPromptStrings(fn);
  }

  // ----- tool-schema extraction (security: unbounded_tool_arg) --------------
  // For a Tool node handler that is (or resolves to) `new ToolNode([t1, t2, …])`,
  // resolve each list element to its `tool(fn, { schema: z.object({…}) })`
  // DEFINITION and convert the zod schema to JSON-schema. The aggregate ToolNode
  // is the in-graph node, so we attach a MERGED args_schema envelope
  // `{type:"object", properties:{<tool>_<field>: …}}` to it — name-namespaced by
  // tool so two tools sharing a field name don't collide. Attaching to the
  // existing aggregate adds NO new nodes (no reachability FP risk). Returns the
  // merged schema map, or null when no element resolves to a real zod-schema tool
  // (so a `new ToolNode([{}, {}])` of non-tool refs — node_types.ts — yields
  // nothing and gains no finding). Modeling note: merging onto the aggregate can
  // only under-report (a bounded field never masks an unbounded one across tools
  // because properties are namespaced), never false-positive.

  // Resolve a single tool list element to its `tool(...)` CallExpression, through
  // one identifier hop (`const searchTool = tool(...)`). Returns the call or null.
  function resolveToolCall(el, seen) {
    if (!el) return null;
    if (
      ts.isCallExpression(el) &&
      ts.isIdentifier(el.expression) &&
      el.expression.text === "tool"
    ) {
      return el;
    }
    if (ts.isIdentifier(el)) {
      const nm = el.text;
      seen = seen || new Set();
      if (seen.has(nm)) return null;
      seen.add(nm);
      const init = varInits.get(nm);
      if (init) return resolveToolCall(init, seen);
    }
    return null;
  }

  // Extract the JSON-schema map from one `tool(fn, { schema: z.object({…}) })`
  // call's options object, or null when no convertible zod schema is present.
  function schemaFromToolCall(call) {
    const opts = call.arguments && call.arguments[1];
    if (!opts || !ts.isObjectLiteralExpression(opts)) return null;
    let toolName = "";
    let schemaExpr = null;
    for (const prop of opts.properties) {
      if (!ts.isPropertyAssignment(prop)) continue;
      let key = "";
      if (ts.isIdentifier(prop.name)) key = prop.name.text;
      else if (ts.isStringLiteralLike(prop.name)) key = prop.name.text;
      if (key === "name") {
        const nm = staticTextOf(prop.initializer);
        if (nm) toolName = nm;
      } else if (key === "schema") {
        schemaExpr = prop.initializer;
      }
    }
    if (!schemaExpr) return null;
    const js = zodToJsonSchema(schemaExpr);
    if (!js) return null;
    return { name: toolName, schema: js };
  }

  // For a Tool node handler arg, build the merged args_schema, or null.
  function extractToolSchemaForHandler(handlerArg) {
    // Resolve the handler to a `new ToolNode([...])` construct (directly, or via
    // a `const toolNode = new ToolNode([...])` identifier hop through varInits).
    let toolNodeExpr = null;
    const resolveCtor = (arg, seen) => {
      if (!arg) return null;
      if (ts.isNewExpression(arg) && ctorName(arg) === "ToolNode") return arg;
      if (ts.isIdentifier(arg)) {
        const nm = arg.text;
        seen = seen || new Set();
        if (seen.has(nm)) return null;
        seen.add(nm);
        const init = varInits.get(nm);
        if (init) return resolveCtor(init, seen);
      }
      return null;
    };
    toolNodeExpr = resolveCtor(handlerArg);
    if (!toolNodeExpr) return null;

    // First arg is the tools list: an inline array, or an identifier bound to one
    // (`const tools = [searchTool, calcTool]`).
    let listExpr = toolNodeExpr.arguments && toolNodeExpr.arguments[0];
    if (listExpr && ts.isIdentifier(listExpr)) {
      const init = varInits.get(listExpr.text);
      if (init) listExpr = init;
    }
    if (!listExpr || !ts.isArrayLiteralExpression(listExpr)) return null;

    const merged = { type: "object", properties: {} };
    let any = false;
    let idx = 0;
    for (const el of listExpr.elements) {
      const call = resolveToolCall(el);
      idx += 1;
      if (!call) continue; // non-tool reference (e.g. `{}`) — skip, no fabrication
      const got = schemaFromToolCall(call);
      if (!got) continue;
      const ns = (got.name || `tool${idx}`).replace(/[^A-Za-z0-9_]/g, "_");
      const props = got.schema && got.schema.properties;
      if (props && typeof props === "object") {
        // z.object root: namespace each field by tool name.
        for (const [field, fieldSchema] of Object.entries(props)) {
          merged.properties[`${ns}_${field}`] = fieldSchema;
          any = true;
        }
      } else {
        // Non-object root schema (rare): attach under the tool name directly.
        merged.properties[ns] = got.schema;
        any = true;
      }
    }
    return any ? merged : null;
  }

  // Harvest `new Command({goto: X})` / `Command({goto: X})` destinations from a
  // handler function body. Returns a Set of resolved destinations where each is
  // either the LG_END sentinel or a string node name. `goto` may be a string
  // literal, the END identifier, a START/END property access, or an array of
  // those. Mirrors the Python `_extract_command_dests_from_return`, adapted to
  // JS's object-literal-argument Command shape. Non-literal goto expressions
  // (a variable, a computed value) are skipped — we can't statically resolve
  // them, and an over-approximated edge to an unknown target is worse than the
  // honest omission.
  function collectCommandGotos(fnNode) {
    const dests = new Set();
    if (!fnNode || !fnNode.body) return dests;
    const harvestGotoValue = (v) => {
      const sent = sentinelOf(v);
      if (sent) {
        dests.add(sent);
        return;
      }
      const s = stringOf(v);
      if (s) {
        dests.add(s);
        return;
      }
      if (ts.isArrayLiteralExpression(v)) {
        for (const el of v.elements) harvestGotoValue(el);
      }
    };
    // Harvest goto destinations from an expression that is actually RETURNED
    // by the handler (codex review 2026-05-31, P2): only a returned Command
    // routes the workflow. A `Command` merely constructed for logging, stored
    // in a local, or built inside a nested helper does NOT — harvesting those
    // would synthesise phantom edges / exits and corrupt cycle analysis.
    // Unwrap parentheses and recurse ternary branches (both arms can return a
    // distinct Command), mirroring the Python shim's return-only extraction.
    const harvestReturnedExpr = (expr) => {
      if (!expr) return;
      if (ts.isParenthesizedExpression(expr)) {
        harvestReturnedExpr(expr.expression);
        return;
      }
      if (ts.isConditionalExpression(expr)) {
        harvestReturnedExpr(expr.whenTrue);
        harvestReturnedExpr(expr.whenFalse);
        return;
      }
      let callee = null;
      let argList = null;
      if (ts.isNewExpression(expr)) {
        callee = expr.expression;
        argList = expr.arguments;
      } else if (ts.isCallExpression(expr)) {
        callee = expr.expression;
        argList = expr.arguments;
      }
      if (!callee || !argList || !argList.length) return;
      let cn = "";
      if (ts.isIdentifier(callee)) cn = callee.text;
      else if (ts.isPropertyAccessExpression(callee)) cn = callee.name.text;
      if (cn === "Command" && ts.isObjectLiteralExpression(argList[0])) {
        for (const prop of argList[0].properties) {
          if (!ts.isPropertyAssignment(prop)) continue;
          let key = "";
          if (ts.isIdentifier(prop.name)) key = prop.name.text;
          else if (ts.isStringLiteralLike(prop.name)) key = prop.name.text;
          if (key === "goto") harvestGotoValue(prop.initializer);
        }
      }
    };
    // Arrow concise body (`(s) => new Command({goto})`) is itself the returned
    // expression; a block body is walked for `return` statements, skipping
    // nested functions (their returns are not the handler's).
    if (!ts.isBlock(fnNode.body)) {
      harvestReturnedExpr(fnNode.body);
      return dests;
    }
    const visit = (n) => {
      if (
        ts.isFunctionDeclaration(n) ||
        ts.isFunctionExpression(n) ||
        ts.isArrowFunction(n)
      ) {
        return; // nested function — its returns aren't this handler's
      }
      if (ts.isReturnStatement(n)) harvestReturnedExpr(n.expression);
      ts.forEachChild(n, visit);
    };
    visit(fnNode.body);
    return dests;
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
              const nodeType = classifyHandler(args[1]);
              const nodeObj = b.ensureNode(
                id,
                name,
                nodeType,
                posOf(sourceFile, node, filePath)
              );
              // Prompt-string extraction (security: secret_in_prompt_template).
              // For LLM nodes, lift the static prompt text out of the handler
              // body into config.system_prompt so the Go rule can scan it for a
              // hardcoded credential. Only set the key when text was found, and
              // never clobber an already-populated value (a node re-registered
              // via a second addNode keeps its first non-empty prompt). Degrade
              // gracefully: no recognisable prompt → key stays unset (ADR-015).
              if (nodeType === "llm" && args[1]) {
                const prompt = extractPromptForHandler(args[1]);
                if (prompt && !nodeObj.config.system_prompt) {
                  nodeObj.config.system_prompt = prompt;
                }
              }
              // Tool-schema extraction (security: unbounded_tool_arg). For a Tool
              // node that is/resolves to `new ToolNode([tool(…), …])`, convert the
              // tools' zod schemas to a merged JSON-schema args_schema so the Go
              // rule can flag an unbounded string/array field. Only attach when a
              // real zod schema resolves (a ToolNode of non-tool refs yields
              // nothing — no new finding); never clobber an existing value.
              if (nodeType === "tool" && args[1]) {
                const schema = extractToolSchemaForHandler(args[1]);
                if (schema && !nodeObj.config.args_schema) {
                  nodeObj.config.args_schema = schema;
                }
              }
              // Attribute this node to the StateGraph root it was added through,
              // so several `new StateGraph(...)` decls merged under one reused
              // variable name (Magic-Resume's three `const workflow = ...`) are
              // detectable as a disjoint-node multi-graph file. The chained
              // `new StateGraph(...).addNode(...)` receiver resolves to its own
              // NewExpression; a later `workflow.addNode(...)` (no inline root)
              // returns null and is simply skipped here (it belongs to whichever
              // root the varname already owns).
              const sgRoot = stateGraphRootOf(recv);
              if (sgRoot) b.recordRootNode(sgRoot, id);
              // Stash the handler AST node so a post-pass can scan its body for
              // `new Command({goto: ...})` dynamic routing (gap 2).
              if (args[1]) b.handlers.set(id, args[1]);
              // Stash the addNode options `{ ends: [...] }` static-routing list.
              // The newer StateGraph API declares a node's possible Command-goto
              // destinations up front via the 3rd-arg `ends` array, so a node
              // whose handler is an opaque/imported reference still gets its
              // outgoing edges. Edges are emitted in the post-pass (after every
              // node is registered) so the known-node gate sees all nodes.
              const opts = args[2];
              if (opts && ts.isObjectLiteralExpression(opts)) {
                for (const prop of opts.properties) {
                  if (!ts.isPropertyAssignment(prop)) continue;
                  let key = "";
                  if (ts.isIdentifier(prop.name)) key = prop.name.text;
                  else if (ts.isStringLiteralLike(prop.name)) key = prop.name.text;
                  if (key !== "ends") continue;
                  if (ts.isArrayLiteralExpression(prop.initializer)) {
                    b.nodeEnds.set(id, prop.initializer);
                  }
                }
              }
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
    if (!src || src === LG_END) return;
    // Conditional edges sourced from START — `addConditionalEdges(START, router,
    // ["A","B"])` (or an object pathMap, or a router returning string literals).
    // Each branch target is a START successor / parallel entry. The pre-fix
    // early-return on `src === LG_START` dropped these entirely, so the lone /
    // first listed successor never became the entry and the rest false-fired
    // unreachable. We harvest every branch target as a START successor; the
    // entry (single node vs synthetic `__start__` fan-out) is resolved later.
    if (src === LG_START) {
      const routerArg = args[1];
      const pathMap = args[2];
      const harvest = (dest) => {
        if (!dest || dest === LG_START || dest === LG_END) return;
        b.addStartSuccessor(nodeId(dest));
      };
      if (pathMap && ts.isObjectLiteralExpression(pathMap)) {
        for (const prop of pathMap.properties) {
          if (!ts.isPropertyAssignment(prop)) continue;
          harvest(sentinelOf(prop.initializer) || stringOf(prop.initializer));
        }
      } else if (pathMap && ts.isArrayLiteralExpression(pathMap)) {
        for (const el of pathMap.elements) {
          harvest(sentinelOf(el) || stringOf(el));
        }
      }
      // No / opaque pathMap: fall back to the router's string-literal returns
      // (best-effort), mirroring the user-node no-pathMap materialisation below.
      let routerFn = null;
      if (routerArg) {
        if (ts.isIdentifier(routerArg)) routerFn = fnDecls.get(routerArg.text) || null;
        else if (ts.isArrowFunction(routerArg) || ts.isFunctionExpression(routerArg))
          routerFn = routerArg;
      }
      if (routerFn) {
        for (const d of collectRouterReturns(routerFn)) harvest(d);
      }
      return;
    }
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
          } else if (b.nodes.has(nodeId(d))) {
            // Only materialise an edge to a DECLARED node. A router (or its
            // return-type annotation) naming a destination with no matching
            // addNode — or an opaque/imported name — must NOT synthesise a
            // phantom edge to a non-existent node, which would corrupt cycle
            // / reachability analysis. Parity with the Command-goto
            // dest-must-be-declared gate (codex review 2026-05-31).
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

  // Command(goto=...) post-pass (gap 2). A node handler that returns
  // `new Command({goto: X})` routes dynamically — a control-flow edge the
  // StateGraph builder calls never declare. For each node with a resolvable
  // handler, scan its body for Command gotos and:
  //   * goto END / "__end__"          -> mark the node has_exit_branch
  //   * goto "<known node>"           -> synthesise an over-approximated edge
  // Edges to destinations that are NOT declared nodes are dropped (mirrors the
  // Python `dest in node_ids` gate) — a dangling edge to an unknown id would
  // be a phantom in the Go rules. Runs only on the selected graph, matching the
  // Python `_augment_runtime_graph_with_command_goto` per-payload augmentation.
  {
    const existing = new Set(best.edges.map((e) => `${e.from} ${e.to}`));
    // `addNode(name, handler, { ends: [...] })` static-routing post-pass. The
    // `ends` array names a node's possible Command-goto destinations
    // declaratively (newer StateGraph API). We materialise an over-approximated
    // outgoing edge to each declared destination, treating END/START sentinels
    // like a Command goto (END -> has_exit_branch, START -> skip). Runs before
    // the Command-goto loop so its `existing` set dedups edges both halves name
    // (the wild `ends:["tools","agent"]` mirrors the handler's Command gotos).
    for (const [nodeIdStr, endsArr] of best.nodeEnds) {
      for (const el of endsArr.elements) {
        const sent = sentinelOf(el);
        const dest = sent || stringOf(el);
        if (!dest) continue;
        if (dest === LG_END) {
          best.markExit(nodeIdStr);
          continue;
        }
        if (dest === LG_START) continue;
        const destId = nodeId(dest);
        if (!best.nodes.has(destId)) continue; // gate: known node only
        const key = `${nodeIdStr} ${destId}`;
        if (existing.has(key)) continue;
        best.edges.push({ from: nodeIdStr, to: destId, condition: "ends" });
        existing.add(key);
      }
    }
    for (const [nodeIdStr, handlerArg] of best.handlers) {
      const fn = resolveHandlerFn(handlerArg);
      if (!fn) continue;
      const dests = collectCommandGotos(fn);
      if (!dests.size) continue;
      for (const dest of dests) {
        if (dest === LG_END) {
          best.markExit(nodeIdStr);
          continue;
        }
        if (dest === LG_START) continue;
        const destId = nodeId(dest);
        if (!best.nodes.has(destId)) continue; // gate: known node only
        const key = `${nodeIdStr} ${destId}`;
        if (existing.has(key)) continue;
        best.edges.push({ from: nodeIdStr, to: destId, condition: "command_goto" });
        existing.add(key);
      }
    }
  }

  // --- entry resolution -----------------------------------------------------
  // Precedence (each step is mutually exclusive with the wild data):
  //   (0) Multi-graph collapse — several `new StateGraph(...)` decls merged
  //       under one reused variable name, covering DISJOINT node sets. There is
  //       no single canonical entry; flag entry_ambiguous + empty entry so
  //       reachability skips (it already skips on EntryAmbiguous && empty entry)
  //       rather than false-flagging graphs 2..N as unreachable. Mirrors the
  //       pydantic-graph multi-`Graph()` disjoint-node-set fix (#36). MUST run
  //       before the START fan-out resolution: the merged builder has one START
  //       successor PER subgraph, and synthesising `__start__` over them would
  //       wrongly conflate the graphs into one (and defeat the skip).
  //   (1) START fan-out — when START routes to >=2 successors, model `__start__`
  //       as a synthetic Control entry node with an edge to each successor so
  //       reachability flows to every parallel entry.
  //   (2) Single START successor / explicit setEntryPoint — entry = that node
  //       (byte-identical to the pre-fix single-entry behaviour: no `__start__`
  //       node is materialised).
  //   (3) Fallback — first registered node.
  let entry = best.entry;
  let entryAmbiguous = false;

  // (0a) File-global multi-graph detection (coarse, robust across builder styles
  // — codex #49). Count distinct `new StateGraph(...)` roots that are assigned to
  // a variable (declaration OR reassignment) or `.compile()`d, across the WHOLE
  // file (not just `best`): >=2 means the file declares multiple independent
  // graphs with no single canonical entry. The per-`best` disjoint check (0b)
  // below only attributes fluent-chain roots, so it misses identifier-receiver /
  // reassigned / different-varname multi-graph files (`let wf = new StateGraph();
  // ...; wf = new StateGraph();` or two `const wf1/wf2`), including ones mixing
  // `addEdge(START,…)` and `setEntryPoint(…)`. Flagging entry_ambiguous makes
  // reachability skip rather than false-flagging the other graphs' nodes; the
  // 1-root START fan-out path is unaffected. Per-graph reachability/cycle coverage
  // on multi-graph files is the deferred multi-graph-emission gap (#42).
  {
    const sgRoots = new Set();
    const walkSG = (n) => {
      if (ts.isVariableDeclaration(n) && n.initializer) {
        const r = stateGraphRootOf(n.initializer);
        if (r) sgRoots.add(r);
      } else if (
        ts.isBinaryExpression(n) &&
        n.operatorToken.kind === ts.SyntaxKind.EqualsToken
      ) {
        const r = stateGraphRootOf(n.right);
        if (r) sgRoots.add(r);
      } else if (
        ts.isCallExpression(n) &&
        ts.isPropertyAccessExpression(n.expression) &&
        n.expression.name.text === "compile"
      ) {
        const r = stateGraphRootOf(n.expression.expression);
        if (r) sgRoots.add(r);
      }
      ts.forEachChild(n, walkSG);
    };
    walkSG(sourceFile);
    if (sgRoots.size >= 2) entryAmbiguous = true;
  }

  // (0b) Disjoint-root multi-graph detection. Only non-empty root sets count; a
  // pair of roots with no shared node id means two genuinely separate graphs
  // (one root can never form a disjoint pair, so single-graph files and the
  // START-fan-out files below can't false-fire). Cheap pairwise check — the
  // root count is tiny.
  {
    const rootSets = [];
    for (const s of best.rootNodeSets.values()) {
      if (s.size) rootSets.push(s);
    }
    for (let i = 0; i < rootSets.length && !entryAmbiguous; i++) {
      for (let j = i + 1; j < rootSets.length; j++) {
        let disjoint = true;
        for (const id of rootSets[i]) {
          if (rootSets[j].has(id)) { disjoint = false; break; }
        }
        if (disjoint) { entryAmbiguous = true; break; }
      }
    }
  }

  if (entryAmbiguous) {
    entry = ""; // no canonical entry across disjoint graphs
  } else if (!entry) {
    // No explicit setEntryPoint — resolve from the START successors.
    const succs = Array.from(best.startSuccessors).filter((id) => best.nodes.has(id));
    if (succs.length >= 2) {
      // START fan-out: synthesise a `__start__` entry that reaches each parallel
      // successor. Type is "parallel" — NOT the "llm" default, and deliberately
      // NOT "control": the Go side aliases the wire-string "control" to
      // NodeTypeLoop, which trips loop_guard ("no MaxIterations") on the
      // synthetic node (a NEW false positive). "parallel" is semantically honest
      // (START runs its successors in parallel) and is inert — no per-type rule
      // gates on NodeTypeParallel, so the synthetic entry fires nothing itself.
      best.ensureNode(
        LG_START_NODE,
        LG_START_NODE,
        "parallel",
        { file: filePath || "", line: 0, col: 0 }
      );
      const existing = new Set(best.edges.map((e) => `${e.from} ${e.to}`));
      for (const dst of succs) {
        const key = `${LG_START_NODE} ${dst}`;
        if (existing.has(key)) continue;
        best.edges.push({ from: LG_START_NODE, to: dst });
        existing.add(key);
      }
      entry = LG_START_NODE;
    } else if (succs.length === 1) {
      entry = succs[0];
    }
  }

  const nodes = Array.from(best.nodes.values());
  // (3) Fallback: first node when no entry resolved and not ambiguous.
  if (!entry && !entryAmbiguous && nodes.length) entry = nodes[0].id;

  const result = {
    nodes,
    edges: best.edges,
    entry_node_id: entry,
    metadata: emptyMeta(filePath),
  };
  if (entryAmbiguous) result.entry_ambiguous = true;
  return result;
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
