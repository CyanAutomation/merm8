# Authentication and Authorization

This document describes how to secure merm8 API endpoints using bearer token authentication.

## Overview

merm8 supports **optional bearer token authentication** for the analyze endpoints. Authentication is only enforced when the server is configured with the `ANALYZE_AUTH_TOKEN` environment variable.

**Key Points**:

- Authentication is **optional** by default (no token configured)
- When configured, authentication is **mandatory** for analyze endpoints in production mode
- Other endpoints (health checks, metrics, documentation) are **never protected** by bearer auth
- All authentication uses the `Authorization: Bearer <token>` HTTP header (RFC 6750)

---

## Configuration

### Server-Side Setup

Authentication is controlled by environment variables:

#### `ANALYZE_AUTH_TOKEN`

- **Type**: String
- **Default**: Empty (authentication disabled)
- **Effect**: When set, the `/v1/analyze*` endpoints require a matching bearer token
- **Usage**:

  ```bash
  export ANALYZE_AUTH_TOKEN="secret-token-here"
  ./server
  ```

#### `DEPLOYMENT_MODE`

- **Type**: String (typically "production" or "development")
- **Default**: "development"
- **Effect**: In production mode, auth is strictly enforced; in development mode, missing/invalid tokens are logged but requests may proceed
- **Usage**:

  ```bash
  export DEPLOYMENT_MODE=production
  export ANALYZE_AUTH_TOKEN="your-secret"
  ./server
  ```

### Token Management

**Best Practices**:

1. **Generate strong tokens**:
   - Use random alphanumeric strings (32+ characters)
   - Example: generate with `openssl rand -hex 32`
2. **Store securely**:
   - Use secrets management systems (e.g., AWS Secrets Manager, HashiCorp Vault, Kubernetes Secrets)
   - Never commit tokens to version control
   - Rotate tokens regularly (e.g., quarterly)
3. **Share with clients**:
   - Distribute out-of-band (separate from API docs)
   - Document expected usage in client integration guides
   - Include rotation schedule

---

## Client-Side Usage

### Making Authenticated Requests

#### Using cURL

```bash
# Without authentication (if server allows)
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{"code":"graph TD\n  A --> B"}'

# With bearer token authentication
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-secret-token" \
  -d '{"code":"graph TD\n  A --> B"}'
```

#### Using JavaScript/TypeScript

```javascript
const token = "your-secret-token";
const response = await fetch("http://localhost:8080/v1/analyze", {
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "Authorization": `Bearer ${token}`,
  },
  body: JSON.stringify({
    code: "graph TD\n  A --> B",
  }),
});

if (response.status === 401) {
  console.error("Authentication failed: missing or invalid token");
  // Handle auth error
}

const data = await response.json();
console.log(data);
```

#### Using Python

```python
import requests

token = "your-secret-token"
url = "http://localhost:8080/v1/analyze"
headers = {
    "Content-Type": "application/json",
    "Authorization": f"Bearer {token}",
}
payload = {
    "code": "graph TD\n  A --> B",
}

response = requests.post(url, json=payload, headers=headers)
if response.status_code == 401:
    print("Authentication failed: missing or invalid token")
    # Handle auth error

data = response.json()
print(data)
```

#### Using Go

```go
package main

import (
    "bytes"
    "io"
    "net/http"
)

func analyzeWithAuth(token string) error {
    client := &http.Client{}
    
    payload := []byte(`{"code":"graph TD\n  A --> B"}`)
    req, err := http.NewRequest("POST", "http://localhost:8080/v1/analyze", bytes.NewReader(payload))
    if err != nil {
        return err
    }
    
    // Add authentication header
    req.Header.Set("Authorization", "Bearer " + token)
    req.Header.Set("Content-Type", "application/json")
    
    resp, err := client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode == http.StatusUnauthorized {
        // Handle 401 response
        body, _ := io.ReadAll(resp.Body)
        // Process error response
    }
    
    return nil
}
```

### Token Storage Patterns

#### Environment Variables (Simple)

```bash
export MERM8_TOKEN="your-secret-token"
```

```python
import os
token = os.getenv("MERM8_TOKEN")
```

#### Configuration File (Secure)

Never store tokens in plain text. Use encrypted config files or external secrets management.

```yaml
# config.yaml (gitignored)
api:
  url: "http://localhost:8080"
  token: "${MERM8_TOKEN}"  # Load from env var at runtime
```

#### Secrets Manager (Recommended for Production)

**AWS Secrets Manager**:

```python
import boto3
import json

def get_token():
    client = boto3.client("secretsmanager")
    response = client.get_secret_value(SecretId="merm8/api-token")
    secret = json.loads(response["SecretString"])
    return secret["token"]

token = get_token()
```

**Kubernetes Secrets**:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: merm8-api-token
type: Opaque
stringData:
  token: your-secret-token
```

```python
# Read from mounted secret
with open("/var/run/secrets/merm8-api-token/token") as f:
    token = f.read().strip()
```

---

## Authentication Errors

### HTTP 401 Unauthorized

**When**: The `Authorization` header is missing, malformed, or the token does not match.

**Response Body**:

```json
{
  "error": {
    "code": "unauthorized",
    "message": "missing or invalid bearer token"
  }
}
```

**Response Headers**:

```
HTTP/1.1 401 Unauthorized
WWW-Authenticate: Bearer realm="merm8-api"
Content-Type: application/json
```

**Troubleshooting**:

| Issue | Cause | Solution |
|-------|-------|----------|
| Missing header | No `Authorization: Bearer ...` sent | Add header to all requests |
| `Bearer` misspelled | Wrong auth scheme (e.g., `Basic` instead of `Bearer`) | Use `Bearer` (case-sensitive) |
| Token mismatch | Client token ≠ server `ANALYZE_AUTH_TOKEN` | Verify token value and ensure loaded correctly |
| Token format | Invalid characters or encoding | Ensure token is plain ASCII/UTF-8 |
| Whitespace | Extra spaces in header value | `Bearer token` not `Bearer  token` (double space) |

### HTTP 403 Forbidden (Reserved)

Currently, merm8 does not differentiate between different levels of authorization (role-based access). This status is reserved for future use if fine-grained permissions are implemented.

---

## Protected Endpoints

### Requiring Authentication

When `ANALYZE_AUTH_TOKEN` is configured, authentication is **required** for:

| Endpoint | Method | Protected |
|----------|--------|-----------|
| `/v1/analyze` | POST | ✅ Yes |
| `/v1/analyze/raw` | POST | ✅ Yes |
| `/v1/analyze/sarif` | POST | ✅ Yes |
| `/v1/analyse` | POST (deprecated) | ✅ Yes |
| `/v1/analyse/raw` | POST (deprecated) | ✅ Yes |
| `/analyze` | POST (legacy) | ✅ Yes |
| `/analyze/raw` | POST (legacy) | ✅ Yes |

### Never Protected

The following endpoints are **never protected** by bearer authentication and are always accessible:

| Endpoint | Purpose |
|----------|---------|
| `/v1/healthz` | Liveness probe |
| `/v1/ready` | Readiness probe |
| `/v1/health/metrics` | Health metrics |
| `/v1/metrics` | Prometheus metrics |
| `/v1/internal/metrics` | Internal metrics (may be firewall-protected at network level) |
| `/v1/info` | Service info |
| `/v1/version` | Version metadata |
| `/v1/rules` | Available rules |
| `/v1/rules/schema` | Rule config schema |
| `/v1/diagram-types` | Supported diagram types |
| `/v1/analyze/help` | Help and templates |
| `/v1/config-versions` | Config version info |
| `/v1/spec` | OpenAPI spec |
| `/v1/docs` | Swagger UI |

**Rationale**: Probes and documentation are intentionally open to support infrastructure monitoring, CI/CD pipelines, and reverse proxy health checks without authentication overhead.

---

## Deployment Patterns

### Single-Service Deployment

For a single API endpoint, embed the token in client environment:

```bash
# Server
export ANALYZE_AUTH_TOKEN="my-secure-token"
./server

# Client
export MERM8_TOKEN="my-secure-token"
python client.py
```

### Multi-Tenant Deployment

For multiple independent services, use a reverse proxy or API gateway to handle authentication:

```
[Client A] ---> [API Gateway] ---> [merm8 Server]
[Client B] ----/              \
[Client C]                     [Shared Server Instance]
```

The gateway can:

- Enforce separate tokens per client
- Rate-limit per client
- Log and audit requests
- Terminate TLS

merm8 can run with `ANALYZE_AUTH_TOKEN` disabled (auth off), trusting the gateway for access control.

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: merm8
spec:
  containers:
  - name: merm8
    image: merm8:latest
    env:
    - name: ANALYZE_AUTH_TOKEN
      valueFrom:
        secretKeyRef:
          name: merm8-secrets
          key: api-token
    - name: DEPLOYMENT_MODE
      value: "production"
    ports:
    - containerPort: 8080
    livenessProbe:
      httpGet:
        path: /v1/healthz
        port: 8080
      initialDelaySeconds: 10
      periodSeconds: 10
    readinessProbe:
      httpGet:
        path: /v1/ready
        port: 8080
      initialDelaySeconds: 5
      periodSeconds: 5
```

---

## Security Considerations

### In-Transit Security (HTTPS/TLS)

Bearer tokens should **always** be transmitted over **HTTPS** in production:

1. **Enforce HTTPS**:
   - Use a reverse proxy (nginx, HAProxy) to terminate TLS
   - Redirect HTTP → HTTPS
   - Set `Strict-Transport-Security` header

2. **Certificate Management**:
   - Use valid, trusted TLS certificates
   - Renew before expiration (e.g., Let's Encrypt with auto-renewal)
   - Monitor certificate validity

3. **Example nginx config**:

   ```nginx
   server {
       listen 80;
       server_name api.example.com;
       return 301 https://$server_name$request_uri;
   }
   
   server {
       listen 443 ssl http2;
       server_name api.example.com;
       
       ssl_certificate /etc/ssl/certs/cert.pem;
       ssl_certificate_key /etc/ssl/private/key.pem;
       ssl_protocols TLSv1.2 TLSv1.3;
       ssl_ciphers HIGH:!aNULL:!MD5;
       ssl_prefer_server_ciphers on;
       add_header Strict-Transport-Security "max-age=31536000" always;
       
       location / {
           proxy_pass http://localhost:8080;
       }
   }
   ```

### Token Security

1. **Prevent token leakage**:
   - Never log full tokens (log first 8 chars + `****` for debugging)
   - Configure log redaction / sanitization
   - Monitor for token exposure in error messages

2. **Implement token rotation**:
   - Rotate tokens regularly (e.g., quarterly)
   - Support multiple tokens during rotation (old + new)
   - Grace period: accept old token for N days after new token deployed

3. **Implement rate limiting**:
   - Limit requests per token to prevent brute-force attacks
   - Implement exponential backoff on repeated failures
   - Example: 5 failed auth attempts → temporary 5-minute block

### Network Security

1. **Firewall/Network Access**:
   - Restrict `/v1/internal/metrics` to trusted internal networks
   - Use VPC/private networks for internal communication
   - Implement network policies in container orchestration

2. **DDoS Mitigation**:
   - Rate limit at reverse proxy level (not just auth level)
   - Use CDN or DDoS protection service
   - Monitor for unusual patterns

---

## Troubleshooting

### "401 Unauthorized" Despite Correct Token

1. **Check token value**:

   ```bash
   # Verify server token
   echo $ANALYZE_AUTH_TOKEN
   
   # Verify client token
   echo $MERM8_TOKEN
   ```

2. **Check header format**:

   ```bash
   # Should be: Authorization: Bearer token123
   # NOT: Authorization: token123
   # NOT: Authorization: Basic token123
   ```

3. **Check for whitespace**:
   - Leading/trailing spaces in token value
   - Double spaces in header

4. **Verify deployment**:
   - Restart server after changing `ANALYZE_AUTH_TOKEN`
   - Confirm new env var is in effect: `curl http://localhost:8080/v1/analyze -X POST -d '{}'` should return 401

### "Authorization: Bearer" Missing

Check that all HTTP clients include the header:

```python
# ❌ Wrong
response = requests.post(url, json=data)

# ✅ Correct
response = requests.post(url, json=data, headers={"Authorization": f"Bearer {token}"})
```

### Server Says "Not Protected" but Expecting Auth

- Verify `ANALYZE_AUTH_TOKEN` environment variable is set
- Confirm `DEPLOYMENT_MODE=production` for strict enforcement
- Check server logs for confirmation

---

## Testing

### Manual Testing with cURL

```bash
# Set up
TOKEN="test-token-secret"
export ANALYZE_AUTH_TOKEN=$TOKEN

# In another terminal, start the server
./server &

# Test with token
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"code":"graph TD\n  A --> B"}' \
  -w "\nStatus: %{http_code}\n"

# Test without token (should fail)
curl -X POST http://localhost:8080/v1/analyze \
  -H "Content-Type: application/json" \
  -d '{"code":"graph TD\n  A --> B"}' \
  -w "\nStatus: %{http_code}\n"
```

### Integration Testing

```bash
#!/bin/bash
# test_auth.sh

TOKEN="test-secret"
URL="http://localhost:8080/v1/analyze"

# Test 1: Successful auth
echo "Test 1: Valid token"
response=$(curl -s -X POST $URL \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"code":"graph TD\n  A --> B"}')
if echo "$response" | grep -q '"valid"'; then
    echo "✓ PASS"
else
    echo "✗ FAIL: $response"
fi

# Test 2: Missing auth
echo "Test 2: Missing token"
response=$(curl -s -X POST $URL \
  -H "Content-Type: application/json" \
  -d '{"code":"graph TD\n  A --> B"}')
if echo "$response" | grep -q '"unauthorized"'; then
    echo "✓ PASS"
else
    echo "✗ FAIL: $response"
fi

# Test 3: Invalid token
echo "Test 3: Invalid token"
response=$(curl -s -X POST $URL \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer wrong-token" \
  -d '{"code":"graph TD\n  A --> B"}')
if echo "$response" | grep -q '"unauthorized"'; then
    echo "✓ PASS"
else
    echo "✗ FAIL: $response"
fi
```

---

## See Also

- [API_GUIDE.md](../API_GUIDE.md) — Complete API reference
- [error-responses.md](error-responses.md) — Error code reference
- [metrics-observability.md](metrics-observability.md) — Metrics endpoints (unprotected)
