#!/usr/bin/env node
import fs from 'node:fs';
import http from 'node:http';
import https from 'node:https';

const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

const signalStarted = async () => {
  const signalURL = process.env.MERM8_PARSE_SIGNAL_URL;
  if (!signalURL) {
    return;
  }

  await new Promise((resolve, reject) => {
    const client = signalURL.startsWith('https://') ? https : http;
    const req = client.request(signalURL, { method: 'POST' }, (res) => {
      res.resume();
      res.on('end', resolve);
    });
    req.on('error', reject);
    req.end();
  });
};

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

  try {
    await signalStarted();
  } catch (err) {
    // Continue parsing even if signal fails
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
