import { isRecord } from "./shared.js";
import { ruleMetadata } from "./rule-metadata.js";

type ConfigError = { code: string; message: string };
const supportedRuleIds = new Set<string>(ruleMetadata.map(rule => rule.id));

export function ruleConfig(value: unknown): Record<string, Record<string, unknown>> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return {};
  const config = value as Record<string, unknown>;
  const nestedRules = config.rules;
  if (nestedRules && typeof nestedRules === "object" && !Array.isArray(nestedRules)) {
    return nestedRules as Record<string, Record<string, unknown>>;
  }
  return config as Record<string, Record<string, unknown>>;
}

function validateMaxFanoutLimit(ruleId: string, options: Record<string, unknown>): ConfigError | null {
  if (ruleId !== "max-fanout" || !("limit" in options)) return null;
  if (Number.isInteger(options.limit) && (options.limit as number) >= 0) return null;
  return { code: "invalid_option", message: "max-fanout.limit must be a non-negative integer" };
}

function validateRuleOptions(ruleId: string, value: unknown): ConfigError | null {
  if (!supportedRuleIds.has(ruleId)) return { code: "unknown_rule", message: `unknown rule: ${ruleId}` };
  if (!isRecord(value)) return { code: "invalid_option", message: `${ruleId} configuration must be an object` };
  if ("enabled" in value && typeof value.enabled !== "boolean") {
    return { code: "invalid_option", message: `${ruleId}.enabled must be a boolean` };
  }
  if ("severity" in value && !["error", "warning", "info"].includes(String(value.severity))) {
    return { code: "invalid_option", message: `${ruleId}.severity must be error, warning, or info` };
  }
  return validateMaxFanoutLimit(ruleId, value);
}

function validateRuleEntries(candidate: Record<string, unknown>, allowSchemaVersion: boolean): ConfigError | null {
  for (const [ruleId, options] of Object.entries(candidate)) {
    if (allowSchemaVersion && ruleId === "schema-version") continue;
    const error = validateRuleOptions(ruleId, options);
    if (error) return error;
  }
  return null;
}

export function validateRuleConfig(value: unknown): ConfigError | null {
  if (value === undefined) return null;
  if (!isRecord(value)) {
    return { code: "invalid_request", message: "config must be an object" };
  }
  const nested = "rules" in value;
  const candidate = nested ? value.rules : value;
  if (!isRecord(candidate)) {
    return { code: "invalid_request", message: "config.rules must be an object" };
  }
  return validateRuleEntries(candidate, !nested);
}
