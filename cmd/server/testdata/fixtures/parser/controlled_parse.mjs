#!/usr/bin/env node
import fs from 'node:fs';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

const waitForFile = async (path) => {
  while (!fs.existsSync(path)) {
    await sleep(10);
  }
};

let input = '';
process.stdin.setEncoding('utf8');
process.stdin.on('data', (chunk) => {
  input += chunk;
});

process.stdin.on('end', async () => {
  const marker = process.env.MERM8_PARSE_MARKER;
  const blockLatch = process.env.MERM8_PARSE_BLOCK_LATCH;
  const releaseLatch = process.env.MERM8_PARSE_RELEASE_LATCH;

  const writeMarker = (line) => {
    if (marker) {
      fs.appendFileSync(marker, `${line}\n`, 'utf8');
    }
  };

  writeMarker('started');

  const isSlow = input.includes('SLOW_PARSE_MARKER');
  const shouldBlock = isSlow && blockLatch && releaseLatch && fs.existsSync(blockLatch);
  if (shouldBlock) {
    writeMarker('blocked');
    await waitForFile(releaseLatch);
    writeMarker('released');
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
