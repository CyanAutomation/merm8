import { createMcpHandler, McpServer } from "@modelcontextprotocol/server";
import { z } from "zod";
import { analyzeMermaid, supportedRules } from "../engine/analyze.js";

const result = (value: unknown) => ({ content: [{ type: "text" as const, text: JSON.stringify(value) }], structuredContent: value });
export const mcpHandler = createMcpHandler(() => {
  const server = new McpServer({ name: "merm8", version: "1.0.0" });
  server.registerTool("analyze_mermaid", { description: "Validate and lint a Mermaid diagram.", inputSchema: { code: z.string().min(1), config: z.record(z.string(), z.record(z.string(), z.unknown())).optional() } }, async ({ code, config }) => result(analyzeMermaid(code, config ?? {})));
  server.registerTool("list_rules", { description: "List Worker-supported merm8 lint rule IDs.", inputSchema: {} }, async () => result({ rules: supportedRules }));
  server.registerTool("list_diagram_types", { description: "List Mermaid diagram families supported by merm8.", inputSchema: {} }, async () => result({ "parser-recognized": ["flowchart", "sequence", "class", "er", "state"], "lint-supported": ["flowchart"] }));
  return server;
}, { legacy: "stateless", responseMode: "json" });
