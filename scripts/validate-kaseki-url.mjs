import { appendFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

export function validateKasekiBaseUrl(value) {
  if (typeof value !== "string" || value.length === 0 || /\s/.test(value)) {
    throw new Error("KASEKI_BASE_URL must be a valid HTTPS URL");
  }

  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error("KASEKI_BASE_URL must be a valid HTTPS URL");
  }

  if (
    url.protocol !== "https:" ||
    !url.hostname ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    value.includes("?") ||
    value.includes("#")
  ) {
    throw new Error(
      "KASEKI_BASE_URL must use HTTPS and must not contain credentials, a query, or a fragment",
    );
  }

  return url.toString().replace(/\/+$/, "");
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const baseUrl = validateKasekiBaseUrl(process.env.KASEKI_BASE_URL);
    if (process.env.GITHUB_ENV) {
      appendFileSync(process.env.GITHUB_ENV, `KASEKI_BASE_URL=${baseUrl}\n`);
    } else {
      console.log(baseUrl);
    }
  } catch (error) {
    console.error(`::error title=Invalid Kaseki URL::${error.message}`);
    process.exitCode = 1;
  }
}
