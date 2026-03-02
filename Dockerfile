# ─────────────────────────────────────────────────────────────────
# Stage 1 – Build the Go binary
# ─────────────────────────────────────────────────────────────────
FROM golang:1.22-alpine AS go-builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/mermaid-lint ./cmd/server

# ─────────────────────────────────────────────────────────────────
# Stage 2 – Lightweight runtime with Node + Mermaid
# ─────────────────────────────────────────────────────────────────
FROM node:20-alpine AS runtime

WORKDIR /app

# Install Mermaid parser dependencies
COPY parser-node/package.json ./parser-node/
RUN cd parser-node && npm install --omit=dev

# Copy parser script
COPY parser-node/parse.mjs ./parser-node/

# Copy compiled Go binary
COPY --from=go-builder /app/mermaid-lint .

ENV PARSER_SCRIPT=/app/parser-node/parse.mjs
ENV PORT=8080

EXPOSE 8080

ENTRYPOINT ["/app/mermaid-lint"]
