#!/usr/bin/env node
setTimeout(() => {
  process.stdout.write(JSON.stringify({ valid: true, ast: { type: "flowchart", direction: "TD", nodes: [], edges: [], subgraphs: [], suppressions: [] } }) + "\n");
}, 8000);
