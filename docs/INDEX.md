# Documentation Index

Complete guide to merm8 documentation, organized by use case.

---

## Quick Navigation

### For New Users

1. Start here: [API Integration Guide](api-integration-guide.md) - Complete integration walkthrough
2. Try examples: [Complete Request/Response Examples](complete-request-response-examples.md) - Real payloads and responses
3. Get help: [Troubleshooting & FAQ](troubleshooting-faq.md) - Common issues and solutions

### For Existing Users (Upgrading)

1. Read: [Migration Guide](migration-guide.md) - Upgrade your config format
2. Check: [API Integration Guide](api-integration-guide.md#api-version-negotiation) - Version negotiation
3. Update: [Configuration Examples](examples/config-examples.md) - Updated config patterns

### For Operators (Running merm8)

1. Setup: [Parser Tuning Guide](parser-tuning.md) - Performance optimization
2. Monitor: [Metrics & Observability](metrics-observability.md) - Health checks and metrics
3. Integrate: [API Integration Guide](api-integration-guide.md#monitoring--observability) - Monitoring setup

### For CI/CD Integration

1. Use: [SARIF Output Examples](examples/sarif-output.md) - Security tool integration
2. Deploy: [API Integration Guide](api-integration-guide.md#sarif-output-format) - GitHub Actions setup
3. Configure: [Parser Tuning Guide](parser-tuning.md) - Optimize for pipelines

---

## Documentation Structure

### Core Guides

#### [API Integration Guide](api-integration-guide.md) ⭐ **Start Here**

Complete integration guide covering:

- Quick start with curl examples
- API version negotiation and headers
- Request/response patterns
- Rate limiting and error handling
- SARIF format integration
- Client code examples (JavaScript, Python, Go)
- Performance tuning and caching strategies
- Monitoring and health checks
- Best practices

**Read time:** 15 minutes  
**Audience:** Developers integrating merm8

---

#### [Complete Request/Response Examples](complete-request-response-examples.md)

11 real-world scenarios with actual JSON:

1. Minimal valid request
2. Single rule validation
3. Multiple rules with config
4. Node suppression
5. Parse error response
6. Config validation error
7. Parser timeout
8. Rate limiting
9. Deprecation warnings
10. Complex enterprise diagram
11. Raw analysis format

Each example includes relevant request and response headers.

**Read time:** 10 minutes  
**Audience:** Developers writing clients and integrations

---

#### [Migration Guide](migration-guide.md)

Step-by-step guide to migrate from legacy configs:

- Deprecation timeline (sunset dates)
- 5 legacy formats still accepted
- 3 before/after conversion examples
- Automated migration script
- 3 common migration scenarios
- Response field migration guide
- Verification and validation tools

**Read time:** 15 minutes  
**Audience:** Users with existing integrations

---

#### [Troubleshooting & FAQ](troubleshooting-faq.md)

Solutions for common issues:

- HTTP status code reference
- 6 common issues with solutions
- 10 frequently asked questions
- Scripts for bulk processing
- Debug procedures

**Read time:** 10 minutes  
**Audience:** Everyone (bookmark this!)

---

### Configuration & Rules

#### [Configuration Examples](examples/config-examples.md)

Configuration format guide:

- Canonical v1 format (recommended)
- Nested format (deprecated)
- Flat format (deprecated)
- Field breakdowns and examples

**Read time:** 5 minutes  
**Audience:** Configuration creators

---

#### [Rule Fix Examples](examples/rule-fixes.md)

Before/after examples for each rule:

- `max-fanout` - Node outgoing edge limits
- `max-depth` - Graph depth constraints
- `no-cycles` - Cycle detection
- `no-disconnected-nodes` - Connectivity checks
- `no-duplicate-node-ids` - Node ID uniqueness
- `rule-id-namespaces` - Namespacing patterns

Each rule shows:

- Violation example
- Issue detection
- Multiple fix strategies

**Read time:** 10 minutes  
**Audience:** Diagram creators and reviewers

---

#### [Rule Suppression Examples](examples/rule-suppressions.md)

Suppression strategies:

- Node-level suppression
- Subgraph-level suppression (experimental)
- Global rule suppression (comment-based)
- Negation (exemption override)
- Examples by rule type

**Read time:** 5 minutes  
**Audience:** Advanced configuration users

---

### Operations & Performance

#### [Parser Tuning Guide](parser-tuning.md)

Performance optimization guide:

- Quick reference table by diagram size
- Key parameters explained
- Environment variable configuration
- Per-request overrides
- Memory and timeout tuning
- Troubleshooting timeouts

**Read time:** 10 minutes  
**Audience:** DevOps and operators

---

#### [Metrics & Observability](metrics-observability.md)

Monitoring and observability:

- Prometheus metrics reference
- Health check endpoints
- Metrics collection patterns
- Alerting examples
- Dashboard setup

**Read time:** 8 minutes  
**Audience:** SREs and operators

---

### Diagram Design

#### [Diagram Templates](examples/diagram-templates.md)

Ready-to-use diagram templates:

- Basic linear flows
- Parallel processes
- Hierarchical systems
- Error handling pipelines
- Microservices architectures

**Read time:** 5 minutes  
**Audience:** Diagram creators

---

#### [Diagram Type Support](diagram-type-support.md)

Supported diagram types and features:

- List of supported types
- Linting capability per type
- Feature availability matrix

**Read time:** 3 minutes  
**Audience:** Everyone

---

### Advanced Topics

#### [SARIF Output Examples](examples/sarif-output.md)

Security Analysis Results Format (SARIF 2.1.0):

- Overview of SARIF format
- 3 detailed SARIF response examples
- GitHub security integration
- Security tool compatibility
- Rule mapping guide

**Read time:** 8 minutes  
**Audience:** Security engineers, CI/CD practitioners

---

#### [Rule ID Namespaces](rule-id-namespaces.md)

Rule ID organization and namespacing:

- Namespace structure (`core/`, `custom/`, etc.)
- Built-in rule IDs by namespace
- Future extensibility patterns
- Migration to namespaced IDs

**Read time:** 5 minutes  
**Audience:** Advanced users, tool integrators

---

#### [Subgraph Namespaces](subgraph-namespaces.md) (Experimental)

Subgraph identification patterns:

- How subgraphs are identified
- Scope and limitations
- Future compatibility notes

**Read time:** 3 minutes  
**Audience:** Advanced configuration users

---

## Use Case Guides

### Getting Started (Day 1)

1. Read: [API Integration Guide](api-integration-guide.md) - Quick start section
2. Try: [Complete Request/Response Examples](complete-request-response-examples.md) - Example 1
3. Build: Make your first request
4. Keep: [Troubleshooting & FAQ](troubleshooting-faq.md) bookmarked

**Time:** 30 minutes

---

### Building Your First Integration (Day 1-2)

1. Read: [API Integration Guide](api-integration-guide.md) - Full guide
2. Study: [Complete Request/Response Examples](complete-request-response-examples.md) - All examples
3. Configure: [Configuration Examples](examples/config-examples.md)
4. Test: [Troubleshooting & FAQ](troubleshooting-faq.md) - Test your setup
5. Deploy: [Parser Tuning Guide](parser-tuning.md) - Configure for production

**Time:** 2-3 hours

---

### Migrating Existing Integration (Day 1)

1. Check: [Migration Guide](migration-guide.md) - Identify your legacy format
2. Convert: Use the provided migration script
3. Validate: [Complete Request/Response Examples](complete-request-response-examples.md) - Example 9 (deprecation)
4. Deploy: Update your integration
5. Verify: Check [Troubleshooting & FAQ](troubleshooting-faq.md) for validation

**Time:** 1-2 hours

---

### Setting Up Monitoring (Day 1)

1. Configure: [Metrics & Observability](metrics-observability.md)
2. Verify health: [API Integration Guide](api-integration-guide.md#health-checks) - Health check endpoints
3. Setup dashboards: [Metrics & Observability](metrics-observability.md) - Dashboard examples
4. Create alerts: [Metrics & Observability](metrics-observability.md) - Alerting patterns

**Time:** 1 hour

---

### CI/CD Integration (Day 1-2)

1. Read: [SARIF Output Examples](examples/sarif-output.md)
2. Setup: [API Integration Guide](api-integration-guide.md#sarif-output-format) - GitHub Actions example
3. Configure: [Parser Tuning Guide](parser-tuning.md) - Performance for pipelines
4. Test: Use [Complete Request/Response Examples](complete-request-response-examples.md)
5. Deploy: Add to your CI/CD pipeline

**Time:** 2-3 hours

---

### Optimizing Performance (Day 2+)

1. Baseline: [Metrics & Observability](metrics-observability.md) - Collect metrics
2. Tune: [Parser Tuning Guide](parser-tuning.md) - All parameters
3. Test: [Parser Tuning Guide](parser-tuning.md) - Benchmarking
4. Monitor: [Metrics & Observability](metrics-observability.md) - Track improvements
5. Maintain: [API Integration Guide](api-integration-guide.md#performance-tuning) - Caching strategies

**Time:** 3-4 hours

---

## Documentation Format Guide

All documentation uses:

- **Headers** (H1-H3) for structure
- **Code blocks** with language tags for syntax highlighting
- **Tables** for quick reference
- **Emphasis** for key points
- **Links** between related docs

### Code Examples

- Language specified: `json`, `bash`, `python`, `go`, `javascript`
- Marked as correct (✅) or incorrect (❌)
- Real-world patterns prioritized
- Comments explain non-obvious parts

### Response Examples

- Actual HTTP headers shown
- Real JSON responses
- Common status codes covered
- Error responses included

---

## Contributing to Documentation

Having issues with the docs? Here's how to help:

1. **Report unclear sections**: [Troubleshooting & FAQ](troubleshooting-faq.md) - Check if it's covered
2. **Suggest improvements**: Open an issue with "docs:" prefix
3. **Add examples**: Submit examples that helped you
4. **Request new guides**: Open an issue describing your use case

---

## Documentation Maintenance

Last updated: **March 2026**

| Document                                                                    | Last Updated | Status     |
| --------------------------------------------------------------------------- | ------------ | ---------- |
| [API Integration Guide](api-integration-guide.md)                           | 2026-03      | ✅ Current |
| [Complete Request/Response Examples](complete-request-response-examples.md) | 2026-03      | ✅ Current |
| [Migration Guide](migration-guide.md)                                       | 2026-03      | ✅ Current |
| [Troubleshooting & FAQ](troubleshooting-faq.md)                             | 2026-03      | ✅ Current |
| [Parser Tuning Guide](parser-tuning.md)                                     | 2026-03      | ✅ Current |
| [Metrics & Observability](metrics-observability.md)                         | 2026-03      | ✅ Current |
| [Configuration Examples](examples/config-examples.md)                       | 2026-03      | ✅ Current |
| [SARIF Output Examples](examples/sarif-output.md)                           | 2026-03      | ✅ Current |
| [Rule Suppression Examples](examples/rule-suppressions.md)                  | 2026-03      | ✅ Current |
| [Diagram Templates](examples/diagram-templates.md)                          | 2026-03      | ✅ Current |

---

## Search Tips

### By Topic

- **API**: [API Integration Guide](api-integration-guide.md), [Complete Request/Response Examples](complete-request-response-examples.md)
- **Config**: [Configuration Examples](examples/config-examples.md), [Migration Guide](migration-guide.md)
- **Rules**: [Rule Fix Examples](examples/rule-fixes.md), [Rule Suppression Examples](examples/rule-suppressions.md)
- **Operations**: [Parser Tuning Guide](parser-tuning.md), [Metrics & Observability](metrics-observability.md)
- **Errors**: [Troubleshooting & FAQ](troubleshooting-faq.md), [Complete Request/Response Examples](complete-request-response-examples.md)

### By Audience

- **Developers**: [API Integration Guide](api-integration-guide.md), [Complete Request/Response Examples](complete-request-response-examples.md)
- **Operators**: [Parser Tuning Guide](parser-tuning.md), [Metrics & Observability](metrics-observability.md)
- **DevOps**: [Parser Tuning Guide](parser-tuning.md), [SARIF Output Examples](examples/sarif-output.md)
- **Architects**: [API Integration Guide](api-integration-guide.md), [Rule ID Namespaces](rule-id-namespaces.md)

---

## External Resources

- **Mermaid**: https://mermaid.js.org - Diagram syntax reference
- **SARIF**: https://sarifweb.azurewebsites.net/ - SARIF format specification
- **Prometheus**: https://prometheus.io/docs - Metrics collection
- **GitHub**: https://github.com/CyanAutomation/merm8 - Source code and issues

---
