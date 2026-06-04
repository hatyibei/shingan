// Two classes define a method named `route`. The graph's router node uses
// `this.route.bind(this)` as its handler. Because `route` is ambiguous (defined
// by >=2 classes), the shim must NOT resolve it to an arbitrary class's body —
// otherwise the decoy class's `goto` is grafted onto this graph. Regression
// guard for codex review #36. (Decoy class is declared FIRST so the old
// first-wins lookup would pick the WRONG body.)
import { StateGraph, MessagesAnnotation, Command } from "@langchain/langgraph";

class Decoy {
  route(state: any) {
    return new Command({ goto: "wrong_target" });
  }
}

class RealGraph {
  route(state: any) {
    return new Command({ goto: "real_target" });
  }
  build() {
    return new StateGraph(MessagesAnnotation)
      .addNode("router", this.route.bind(this))
      .addNode("real_target", (s: any) => s)
      .addNode("wrong_target", (s: any) => s)
      .addEdge("__start__", "router")
      .addEdge("real_target", "__end__")
      .addEdge("wrong_target", "__end__")
      .compile();
  }
}
