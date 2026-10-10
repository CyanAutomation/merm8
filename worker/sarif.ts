import type { analyzeMermaid } from "../src/engine/analyze.js";
import { ruleMetadata } from "./rule-metadata.js";

const sarifLevel = (severity: string): string => severity === "error" ? "error" : severity === "warning" ? "warning" : "note";

export function toSarif(code: string, analysis: ReturnType<typeof analyzeMermaid>) {
  const rules = new Map<string, { id: string; shortDescription: { text: string }; defaultConfiguration: { level: string } }>();
  for (const item of analysis.issues) {
    if (rules.has(item["rule-id"])) continue;
    const metadata = ruleMetadata.find(rule => rule.id === item["rule-id"]);
    rules.set(item["rule-id"], {
      id: item["rule-id"],
      shortDescription: { text: metadata?.description ?? item["rule-id"] },
      defaultConfiguration: { level: sarifLevel(item.severity) },
    });
  }
  const results = analysis.issues.map(item => ({
    ruleId: item["rule-id"],
    level: sarifLevel(item.severity),
    message: { text: item.message },
    locations: [{ physicalLocation: {
      artifactLocation: { uri: "diagram.mmd" },
      ...(item.line ? { region: { startLine: item.line, ...(item.column ? { startColumn: item.column } : {}) } } : {}),
    } }],
    partialFingerprints: { issueFingerprint: item.fingerprint },
  }));
  return {
    $schema: "https://json.schemastore.org/sarif-2.1.0.json",
    version: "2.1.0",
    runs: [{
      tool: { driver: {
        name: "merm8",
        informationUri: "https://github.com/CyanAutomation/merm8",
        rules: [...rules.values()],
      } },
      ...(results.length ? { artifacts: [{ location: { uri: "diagram.mmd" }, contents: { text: code } }] } : {}),
      results,
      invocations: [{ executionSuccessful: analysis.valid, properties: { "request-uri": "/v1/analyze/sarif" } }],
    }],
  };
}
