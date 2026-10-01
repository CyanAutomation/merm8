import { fileURLToPath } from "node:url";
import path from "node:path";

export function validateKasekiRunId(value: unknown): string {
  if (typeof value !== "string" || !/^[A-Za-z0-9_-]{1,128}$/.test(value)) {
    throw new Error("Kaseki returned an invalid run ID");
  }

  return value;
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    console.log(validateKasekiRunId(process.env.KASEKI_RUN_ID));
  } catch (error) {
    const message = error instanceof Error ? error.message : String(error);
    console.error(`::error title=Invalid Kaseki run ID::${message}`);
    process.exitCode = 1;
  }
}
