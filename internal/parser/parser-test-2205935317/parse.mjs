#!/usr/bin/env node
process.stdout.write(JSON.stringify({
  valid: true,
  diagram_type: "sequence",
  ast: {
    type: "unknown",
    direction: "TD",
    nodes: [],
    edges: [],
    subgraphs: [],
    suppressions: []
  }
}) + "\n");
process.exit(0);
