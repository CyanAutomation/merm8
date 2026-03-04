#!/usr/bin/env node
import fs from 'node:fs';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

let input = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (chunk) => {
  input += chunk;
});

process.stdin.on('end', async () => {
  const marker = process.env.MERM8_PARSE_MARKER;
  if (marker) {
    fs.appendFileSync(marker, 'started\n', 'utf8');
  }

  const isSlow = input.includes('SLOW_PARSE_MARKER');
  if (isSlow) {
    await sleep(1800);
  }

  const result = {
    valid: true,
    ast: {
      type: 'flowchart',
      direction: 'TD',
      nodes: [{ id: 'A' }, { id: 'B' }],
      edges: [{ from: 'A', to: 'B' }],
      subgraphs: [],
      suppressions: [],
    },
  };

  process.stdout.write(`${JSON.stringify(result)}\n`);
});
