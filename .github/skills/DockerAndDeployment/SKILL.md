# Docker & Deployment

## Overview

Containerizing the merm8 application and deploying it in isolated environments. This skill covers Docker fundamentals, multi-container setup with Docker Compose, environment configuration, and deployment best practices.

## Learning Objectives

- [ ] Understand Docker image creation and container lifecycle
- [ ] Use Docker Compose to orchestrate services
- [ ] Configure environment variables for different deployments
- [ ] Build and optimize Docker images
- [ ] Deploy containers to production environments

## Key Concepts

### Docker Fundamentals

- **Image vs Container**: Image is blueprint; container is running instance
- **Dockerfile layers**: Each instruction adds a cached layer

### Multi-stage Build

```dockerfile
FROM golang:1.24-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o server ./cmd/server/main.go

FROM node:20-alpine
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/server /app/server
COPY parser-node/ /app/parser-node/
WORKDIR /app
EXPOSE 8080
CMD ["./server"]
```

### Docker Compose

```yaml
version: "3.9"
services:
  merm8:
    build: .
    ports:
      - "8080:8080"
    environment:
      PORT: 8080
      PARSER_SCRIPT: /app/parser-node/parse.mjs
```

## Relevant Code in merm8

| Component  | Location                  | Purpose                                       |
| ---------- | ------------------------- | --------------------------------------------- |
| Dockerfile | Dockerfile                | Multi-stage build for Go server + Node parser |
| Compose    | docker-compose.yml        | Local development service                     |
| Smoke test | smoke-test.sh             | Post-deploy verification                      |
| Server     | cmd/server/main.go        | Reads PORT env var                            |
| Parser     | internal/parser/parser.go | Reads PARSER_SCRIPT env var                   |

## Development Workflow

### Building Locally

```bash
docker build -t merm8:latest .
docker run -p 8080:8080 merm8:latest
```

### Using Docker Compose

```bash
docker-compose up -d
docker-compose logs -f
docker-compose down
```

### Testing Deployed Container

```bash
curl http://localhost:8080/analyze -X POST -H 'Content-Type: application/json' \
  -d '{"code":"graph TD\n  A-->B"}'
bash smoke-test.sh
```

## Deployment Scenarios

### Production Steps

1. Push image to registry (ECR/GCR/ACR)
2. Deploy via ECS/GKE/AKS
3. Set env vars (PORT, PARSER_SCRIPT)
4. Configure resource limits and health checks

### Kubernetes Example

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: merm8
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: merm8
          image: gcr.io/myproject/merm8:latest
          ports:
            - containerPort: 8080
          env:
            - name: PORT
              value: "8080"
```

## Common Tasks

### Updating Dependencies

```bash
go get -u ./...
go mod tidy
cd parser-node
npm update
```

### Debugging Containers

```bash
docker run -it merm8:latest /bin/sh
docker logs <container>
docker stats <container>
```

### Optimizing Images

- Use smaller base images (Alpine)
- Use multi-stage builds to drop build tools
- Order instructions to maximize cache

## Resources & Best Practices

- Always pin dependency versions
- Use non-root users inside containers
- Scan images for vulnerabilities
- Keep build context small
- Document Docker commands and env vars

## Prerequisites

- Docker + Docker Compose installed
- Familiarity with container lifecycle
- Basic networking (ports, env vars)

## Related Skills

- Systems Programming & Process Management for subprocess control
- Go Backend Development for server configuration
