// Command(goto=...) dynamic routing (gap 2). Node handlers return
// `new Command({goto: X})` to route at runtime — control flow the StateGraph
// builder calls never declare statically.
//
//   START -> worker
//   worker -> Command({goto: END | "other"})   (dynamic)
//   other  -> Command({goto: worker})           (cycle back, dynamic)
//
// worker's Command(goto: END) gives it has_exit_branch=true, so the
// worker<->other cycle downgrades cycle_detection from Critical to Warning.
// worker's Command(goto: "other") synthesises a worker->other edge; other's
// Command(goto: "worker") synthesises the other->worker edge closing the cycle.
import { StateGraph, START, END, Command } from "@langchain/langgraph";

const graph = new StateGraph({ channels: {} });
graph.addNode("worker", worker);
graph.addNode("other", other);
graph.addEdge(START, "worker");

function worker(state: any) {
  if (state.done) {
    return new Command({ goto: END, update: { result: state.x } });
  }
  return new Command({ goto: "other", update: {} });
}

function other(state: any) {
  return new Command({ goto: "worker" });
}

export const app = graph.compile();
