// fallow-ignore-file unused-file
import {
  appendFileSync,
  existsSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { createInterface } from "node:readline";

type ParserResult = {
  valid: boolean;
  diagram_type?: string;
  ast?: {
    type: string;
    direction: string;
    nodes: Array<{ id: string; label: string }>;
    edges: Array<{ from: string; to: string; type?: string }>;
    subgraphs: unknown[];
    suppressions: unknown[];
  };
  error?: { message: string; line: number; column: number };
};

type WorkerRequest = {
  id?: string;
  code?: string;
  timeout_ms?: number;
};

const mode = process.env.MERM8_TEST_PARSER_MODE ?? "success";
const versionInfo = {
  parser_version: "bridge-v2",
  mermaid_version: "12.0.0",
};

function resultWithNodes(
  nodes: Array<{ id: string; label: string }> = [{ id: "n", label: "oneshot" }],
): ParserResult {
  return {
    valid: true,
    diagram_type: "flowchart",
    ast: {
      type: "flowchart",
      direction: "TD",
      nodes,
      edges: [],
      subgraphs: [],
      suppressions: [],
    },
  };
}

function writeJSON(value: unknown, newline = true): void {
  process.stdout.write(`${JSON.stringify(value)}${newline ? "\n" : ""}`);
}

function appendCounter(path: string | undefined, value: string): void {
  if (path) appendFileSync(path, value, "utf8");
}

function readStartCount(path: string | undefined): number {
  if (!path) return 0;
  try {
    return Number.parseInt(readFileSync(path, "utf8"), 10) || 0;
  } catch {
    return 0;
  }
}

async function readStdin(): Promise<string> {
  return new Promise((resolve, reject) => {
    let input = "";
    process.stdin.setEncoding("utf8");
    process.stdin.on("data", (chunk: string) => {
      input += chunk;
    });
    process.stdin.on("end", () => resolve(input));
    process.stdin.on("error", reject);
  });
}

function keepProcessAlive(): void {
  setInterval(() => undefined, 1000);
}

function failFirstVersionLookup(): void {
  const marker = process.env.VERSION_FAILURE_MARKER;
  if (
    process.env.MERM8_TEST_FAIL_FIRST_VERSION === "1" &&
    marker &&
    !existsSync(marker)
  ) {
    writeFileSync(marker, "failed", "utf8");
    process.stderr.write("version unavailable");
    process.exit(1);
  }
}

function recordVersionLookup(): void {
  appendCounter(process.env.VERSION_COUNTER, "version\n");
  failFirstVersionLookup();
}

function handleVersionInfo(): void {
  if (mode === "version-cache") recordVersionLookup();
  const rawOutput = process.env.MERM8_TEST_VERSION_OUTPUT;
  if (rawOutput !== undefined) {
    process.stdout.write(rawOutput);
    process.exit(0);
  }

  writeJSON(versionInfo, false);
}

function shouldFailCacheOnce(): boolean {
  const failMarker = process.env.FAIL_MARKER;
  return Boolean(
    process.env.FAIL_INTERNAL_ONCE === "1" &&
    failMarker &&
    !existsSync(failMarker)
  );
}

function failCacheOnce(): ParserResult {
  const failMarker = process.env.FAIL_MARKER;
  if (failMarker) {
    writeFileSync(failMarker, "done", "utf8");
  }
  process.stderr.write("simulated internal failure");
  process.exitCode = 1;
  return {
    valid: false,
    error: {
      message: "internal parser error: simulated",
      line: 1,
      column: 1,
    },
  };
}

function isMalformedCacheInput(input: string): boolean {
  return input.includes("-->") && input.trim().endsWith("-->");
}

function syntaxErrorResult(): ParserResult {
  return {
    valid: false,
    error: { message: "Syntax error", line: 2, column: 4 },
  };
}

function validCacheResult(): ParserResult {
  return {
    valid: true,
    diagram_type: "flowchart",
    ast: {
      type: "flowchart",
      direction: "TD",
      nodes: [
        { id: "a", label: "A" },
        { id: "b", label: "B" },
      ],
      edges: [{ from: "a", to: "b", type: "-->" }],
      subgraphs: [],
      suppressions: [],
    },
  };
}

function memoryLimitResult(): ParserResult {
  process.stderr.write(
    "FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory\n",
  );
  return {
    valid: false,
    error: { message: "internal parser error: oom", line: 0, column: 0 },
  };
}

function topLevelDiagramTypeResult(): ParserResult {
  return {
    valid: true,
    diagram_type: "sequence",
    ast: {
      type: "unknown",
      direction: "TD",
      nodes: [],
      edges: [],
      subgraphs: [],
      suppressions: [],
    },
  };
}

function astExtractionErrorResult(): ParserResult {
  return {
    valid: false,
    error: {
      message: "AST extraction failed in parser runtime: synthetic fixture",
      line: 0,
      column: 0,
    },
  };
}

function subprocessInternalErrorResult(): ParserResult {
  return {
    valid: false,
    error: {
      message: "internal parser error: exploded",
      line: 0,
      column: 0,
    },
  };
}

const oneShotResultHandlers: Record<string, () => ParserResult> = {
  "top-level-diagram-type": topLevelDiagramTypeResult,
  "ast-extraction-error": astExtractionErrorResult,
  "valid-false-without-error": () => ({ valid: false }),
  "valid-without-ast": () => ({ valid: true }),
  "subprocess-internal-error": subprocessInternalErrorResult,
  "memory-limit": memoryLimitResult,
  cache: () => {
    if (shouldFailCacheOnce()) return failCacheOnce();
    if (isMalformedCacheInput(inputBuffer)) return syntaxErrorResult();
    return validCacheResult();
  },
  "version-cache": () => resultWithNodes([]),
};

function oneShotResult(): ParserResult {
  return (oneShotResultHandlers[mode] ?? (() => resultWithNodes()))();
}

let inputBuffer = "";
let workerStartCount = 1;

async function runOneShot(): Promise<void> {
  inputBuffer = await readStdin();

  if (mode === "timeout") {
    keepProcessAlive();
    return;
  }

  await waitForOneShotMode();

  if (mode === "version-cache") {
    appendCounter(process.env.PARSE_COUNTER, "parse\n");
  }

  if (writeRawParserResponse()) return;

  writeJSON(oneShotResult());
  setOneShotExitCode();
}

async function waitForCacheMode(): Promise<void> {
  appendCounter(process.env.COUNTER_FILE, "parse\n");
  const delay = process.env.SLOW_PARSER === "1" ? 1500 : 120;
  await new Promise((resolve) => setTimeout(resolve, delay));
}

async function waitForDelayedSuccessMode(): Promise<void> {
  const delay = Number(process.env.MERM8_TEST_PARSER_DELAY_MS ?? "25");
  await new Promise((resolve) => setTimeout(resolve, delay));
}

const oneShotWaitHandlers: Record<string, () => Promise<void>> = {
  cache: waitForCacheMode,
  "delayed-success": waitForDelayedSuccessMode,
};

async function waitForOneShotMode(): Promise<void> {
  await (oneShotWaitHandlers[mode] ?? (async () => undefined))();
}

function writeRawParserResponse(): boolean {
  const rawResponse = process.env.MERM8_TEST_PARSER_RAW_RESPONSE;
  if (rawResponse !== undefined) {
    process.stdout.write(rawResponse);
    return true;
  }
  return false;
}

function setOneShotExitCode(): void {
  if (mode === "subprocess-internal-error" || mode === "memory-limit") {
    process.exitCode = 1;
  }
}

type WorkerHandler = (request: WorkerRequest) => Promise<boolean>;

async function handleNoNewlineWorker(request: WorkerRequest): Promise<boolean> {
  if (mode !== "worker-no-newline" || !request.code?.includes("NO_NEWLINE")) {
    return false;
  }
  process.stdout.write(
    `{"id":${JSON.stringify(request.id ?? "")},"result":{"valid":true}`,
  );
  keepProcessAlive();
  return true;
}

async function handleTimeoutEnvelope(request: WorkerRequest): Promise<boolean> {
  if (mode !== "worker-timeout-envelope") return false;
  writeJSON({
    id: request.id ?? "",
    error: `parser_timeout: exceeded ${String(request.timeout_ms ?? 0)}ms`,
  });
  return true;
}

async function delaySlowWorkerReplacement(request: WorkerRequest): Promise<boolean> {
  if (mode !== "worker-replacement" || !request.code?.includes("SLOW")) {
    return false;
  }
  const delay = Number(process.env.MERM8_TEST_PARSER_SLOW_DELAY_MS ?? "1300");
  await new Promise((resolve) => setTimeout(resolve, delay));
  return false;
}

async function handleWorkerInvalidResult(
  request: WorkerRequest,
): Promise<boolean> {
  if (mode !== "worker-valid-false-without-error") return false;
  writeJSON({ id: request.id ?? "", result: { valid: false } });
  return true;
}

function workerLabel(): string {
  const labels: Record<string, string> = {
    "worker-process-id": String(process.pid),
    "worker-replacement": String(workerStartCount),
    "worker-close": "A",
  };
  return labels[mode] ?? "ok";
}

async function handleWorkerRequest(request: WorkerRequest): Promise<void> {
  const handlers: WorkerHandler[] = [
    handleNoNewlineWorker,
    handleTimeoutEnvelope,
    delaySlowWorkerReplacement,
    handleWorkerInvalidResult,
  ];
  for (const handler of handlers) {
    if (await handler(request)) return;
  }
  writeJSON({
    id: request.id ?? "",
    result: resultWithNodes([{ id: "n", label: workerLabel() }]),
  });
}

async function runWorker(): Promise<void> {
  const rl = createInterface({
    input: process.stdin,
    crlfDelay: Infinity,
    terminal: false,
  });

  for await (const line of rl) {
    await handleWorkerRequest(JSON.parse(line) as WorkerRequest);
  }
}

async function main(): Promise<void> {
  if (process.argv.includes("--version-info")) {
    handleVersionInfo();
    return;
  }

  if (process.argv.includes("--worker")) {
    const counterPath = process.env.MERM8_TEST_PARSER_START_COUNTER;
    if (counterPath) {
      workerStartCount = readStartCount(counterPath) + 1;
      writeFileSync(counterPath, String(workerStartCount), "utf8");
    }
    await runWorker();
    return;
  }

  await runOneShot();
}

await main();
