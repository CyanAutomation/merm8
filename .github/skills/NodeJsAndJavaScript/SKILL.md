# Node.js & JavaScript

## Overview
Building and maintaining the Mermaid parser bridge that converts diagram code into an abstract syntax tree. This involves Node.js async patterns, the official Mermaid library, and process communication via stdin/stdout.

## Learning Objectives
- [ ] Understand Node.js module system and async/await patterns
- [ ] Learn how to use the official Mermaid library programmatically
- [ ] Implement stdin/stdout communication for inter-process communication
- [ ] Handle JSON serialization in Node.js
- [ ] Set up JSDOM for Node.js environments

## Key Concepts

### Mermaid Library Integration
The official Mermaid npm package provides diagram parsing:
```javascript
import mermaid from 'mermaid';

async function parseDiagram(code) {
    const diagram = await mermaid.parse(code);
    return diagram;
}
```

### Stdin/Stdout Communication
Node.js parser receives diagram code via stdin and returns JSON via stdout:
```javascript
process.stdin.on('data', async (chunk) => {
    const code = chunk.toString();
    const diagram = await mermaid.parse(code);
    process.stdout.write(JSON.stringify(diagram));
});
```

### JSDOM Setup
Mermaid needs a DOM environment. JSDOM provides this in Node.js:
```javascript
import { JSDOM } from 'jsdom';

const dom = new JSDOM();
global.document = dom.window.document;
global.window = dom.window;
```

### Error Handling
Mermaid throws on invalid syntax; the parser must catch and return structured errors:
```javascript
try {
    const diagram = await mermaid.parse(code);
} catch (error) {
    process.stdout.write(JSON.stringify({
        error: error.message,
        name: error.name
    }));
}
```

## Relevant Code in merm8

| Component | Location | Purpose |
|-----------|----------|---------|
| Parser script | parser-node/parse.mjs | Node.js entry point |
| Package config | parser-node/package.json | Dependencies (mermaid, jsdom) |
| Go parser wrapper | internal/parser/parser.go | Spawns this script |

## Development Workflow

### Modifying the Parser Script
```bash
cd parser-node
npm install
node parse.mjs
```

### Testing Parser Changes
1. Update parse.mjs
2. Pipe sample diagram: `echo 'graph TD\n  A-->B' | node parse.mjs`
3. Verify JSON output is valid
4. Run Go tests for integration

### Managing Dependencies
```bash
cd parser-node
npm install mermaid@latest
npm ls
```

## Common Tasks

### Adding New Diagram Type Support
- Mermaid handles types automatically; parser just passes through metadata.

### Improving Error Messages
```javascript
catch (error) {
    const parsed = {
        valid: false,
        error: {
            message: error.message,
            line: extractLineNumber(error),
            column: extractColumnNumber(error)
        }
    };
    process.stdout.write(JSON.stringify(parsed));
}
```

### Debugging Parser Issues
```bash
DEBUG=mermaid:* node parse.mjs
echo 'graph TD\n  A-->B' | node parse.mjs
```

## Process Communication Protocol

### Input Format
Plain text Mermaid diagram code.

### Output Format
JSON object with `diagram`, `error`, and metadata (type, nodes, edges).

## Resources & Best Practices
- **Async/Await** for clean Mermaid calls
- **Input Validation**: ensure code is non-empty
- **Timeout Awareness**: Go wrapper enforces 2s deadline
- **Version Pinning**: Lock Mermaid version in `package.json`

## Prerequisites
- JavaScript ES6+ (modules, async/await)
- Node.js basics (process, event loop)
- JSON serialization
- npm usage (`package.json`, `npm install`)

## Related Skills
- Systems Programming & Process Management for Go integration
- Mermaid Diagram Specification for syntax context