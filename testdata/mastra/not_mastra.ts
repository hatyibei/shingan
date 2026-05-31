// A plain TypeScript file with no Mastra workflow. The shim must yield an empty
// graph (no nodes, no edges) and the worker must survive (robustness contract).
export function add(a: number, b: number): number {
  return a + b;
}

const greeting = "hello";
console.log(greeting, add(1, 2));
