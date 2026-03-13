import { JSDOM } from "jsdom";
const { window: _win } = new JSDOM("<!DOCTYPE html>");
global.window = _win;
global.document = _win.document;
global.Element = _win.Element;
global.HTMLElement = _win.HTMLElement;
global.DocumentFragment = _win.DocumentFragment;
global.NodeFilter = _win.NodeFilter;
global.Node = _win.Node;

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
