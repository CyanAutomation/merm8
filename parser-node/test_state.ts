import { JSDOM } from "jsdom";
const { window: _win } = new JSDOM("<!DOCTYPE html>");
const runtimeGlobals = globalThis as unknown as Record<string, unknown>;
runtimeGlobals.window = _win;
runtimeGlobals.document = _win.document;
runtimeGlobals.Element = _win.Element;
runtimeGlobals.HTMLElement = _win.HTMLElement;
runtimeGlobals.DocumentFragment = _win.DocumentFragment;
runtimeGlobals.NodeFilter = _win.NodeFilter;
runtimeGlobals.Node = _win.Node;

const mermaid = (await import("mermaid/dist/mermaid.core.mjs")).default;
mermaid.initialize({ startOnLoad: false });

const source = `stateDiagram-v2
    [*] --> State1
    State1 --> State2: event
    State2 --> State1: event`;

const diagram = await mermaid.mermaidAPI.getDiagramFromText(source);
const db = diagram?.db;
console.log("DB keys:", Object.keys(db || {}));
console.log("DB:", JSON.stringify(db, null, 2).slice(0, 3000));
