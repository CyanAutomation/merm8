# Systems Programming & Process Management

## Overview

Managing subprocess lifecycle, inter-process communication, and system resource constraints. This skill covers spawning Node.js from Go, handling I/O, managing timeouts, and ensuring clean process termination.

## Learning Objectives

- [ ] Understand subprocess creation and lifecycle management
- [ ] Implement stdin/stdout-based inter-process communication
- [ ] Use context for timeouts and cancellation
- [ ] Handle process signals and graceful termination
- [ ] Debug subprocess I/O and error streams

## Key Concepts

### Subprocess Architecture

merm8 spawns a Node.js process to parse diagrams.

### Process Invocation

```go
cmd := exec.CommandContext(ctx, "node", "parse.mjs")
cmd.Stdin = strings.NewReader(diagramCode)
cmd.Stdout = &stdout
cmd.Stderr = &stderr
err := cmd.Run()
```

### Context-Based Timeouts

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()
cmd := exec.CommandContext(ctx, "node", "parse.mjs")
```

### I/O Handling

- **STDIN**: Write diagram code
- **STDOUT**: Read JSON AST
- **STDERR**: Capture errors for debugging

## Relevant Code in merm8

| Component      | Location                  | Purpose                           |
| -------------- | ------------------------- | --------------------------------- |
| Parser wrapper | internal/parser/parser.go | Subprocess lifecycle + timeout    |
| Main server    | cmd/server/main.go        | HTTP server calling parser        |
| Node script    | parser-node/parse.mjs     | Subprocess entry point            |
| Docker setup   | Dockerfile                | Container environment for Node.js |

## Development Workflow

### Testing Subprocess Behavior

```bash
echo "graph TD\n  A-->B" | node parser-node/parse.mjs
```

### Debugging I/O Issues

Capture stderr:

```go
stderr := &bytes.Buffer{}
cmd.Stderr = stderr
err := cmd.Run()
if err != nil {
    log.Printf("stderr: %s", stderr.String())
}
```

### Monitoring Resources

Use `ps`, `docker stats`, or `top` during parser execution.

## Common Tasks

### Changing Timeout

Adjust constant in parser wrapper:

```go
const parseTimeout = 2 * time.Second
```

### Handling Crashes

Return structured error when parser fails to run.

### Environment Variables in Subprocess

```go
cmd.Env = append(os.Environ(), "DEBUG=mermaid:*")
```

## I/O Patterns

### Writing to Stdin

```go
stdin, _ := cmd.StdinPipe()
go func() {
    io.WriteString(stdin, diagramCode)
    stdin.Close()
}()
```

### Reading from Stdout

```go
stdout, _ := cmd.StdoutPipe()
output, _ := io.ReadAll(stdout)
json.Unmarshal(output, &result)
```

### Capturing Stderr

```go
stderr := &bytes.Buffer{}
cmd.Stderr = stderr
cmd.Run()
```

## Deployment Considerations

### Environment Variables

- `PARSER_SCRIPT`: path to `parse.mjs`
- `PORT`: server port

### Resource Limits

Limit memory/CPU for parser processes.

### Docker Environment

Parser runs inside container; ensure Node.js installed.

## Resources & Best Practices

- Always use timeouts
- Validate parser output before returning
- Cancel contexts on shutdown
- Log stderr when errors occur

## Prerequisites

- Go's `os/exec` + `context`
- UNIX pipes (stdin/stdout)
- Basic debugging tools (`ps`, `top`)

## Related Skills

- Go Backend Development for API flow
- Docker & Deployment for containerized processes
