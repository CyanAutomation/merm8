import { appendFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

export function validateKasekiBaseUrl(value, allowedHostsValue) {
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

  if (typeof allowedHostsValue !== "string" || allowedHostsValue.length === 0) {
    throw new Error(
      "KASEKI_ALLOWED_HOSTS must list approved controller hostnames",
    );
  }

  const allowedHosts = allowedHostsValue.split(",").map((entry) => {
    const candidate = entry.trim();
    let allowedUrl;
    try {
      allowedUrl = new URL(`https://${candidate}`);
    } catch {
      throw new Error(
        "KASEKI_ALLOWED_HOSTS must be a comma-separated list of hostnames",
      );
    }

    const isHostname =
      /^(?=.{1,253}$)(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))*$/i.test(
        candidate,
      );
    if (
      !isHostname ||
      allowedUrl.hostname !== candidate.toLowerCase() ||
      allowedUrl.port ||
      allowedUrl.pathname !== "/" ||
      allowedUrl.search ||
      allowedUrl.hash
    ) {
      throw new Error(
        "KASEKI_ALLOWED_HOSTS must be a comma-separated list of hostnames",
      );
    }

    return allowedUrl.hostname;
  });

  if (!allowedHosts.includes(url.hostname)) {
    throw new Error(
      "KASEKI_BASE_URL host must match a hostname in KASEKI_ALLOWED_HOSTS",
    );
  }

  return url.toString().replace(/\/+$/, "");
}

if (
  process.argv[1] &&
  path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)
) {
  try {
    const baseUrl = validateKasekiBaseUrl(
      process.env.KASEKI_BASE_URL,
      process.env.KASEKI_ALLOWED_HOSTS,
    );
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
