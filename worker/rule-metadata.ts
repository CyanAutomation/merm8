export const ruleMetadata = [
  { id: "max-fanout", description: "Limit the number of outgoing connections from a node.", severity: "warning", "default-config": { limit: 5 } },
  { id: "no-cycles", description: "Disallow cycles in flowcharts.", severity: "error" },
  { id: "no-disconnected-nodes", description: "Disallow nodes that have no connections.", severity: "error" },
  { id: "no-duplicate-node-ids", description: "Disallow repeated node IDs.", severity: "error" },
  { id: "no-undefined-actors", description: "Require sequence diagram actors to be declared.", severity: "error", "diagram-type": "sequence" },
  { id: "no-duplicate-classes", description: "Disallow repeated class declarations.", severity: "error", "diagram-type": "class" },
  { id: "no-self-referential", description: "Flag entity relationships that point back to the same entity.", severity: "warning", "diagram-type": "er" },
  { id: "no-unreachable-state", description: "Disallow states that cannot be reached from the initial state.", severity: "error", "diagram-type": "state" },
] as const;
