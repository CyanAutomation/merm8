# Repository skills

Use the skill whose trigger best matches the requested change. For work that crosses components, apply the owning implementation skill together with the relevant contract or workflow skill. Read the current source and canonical docs because these guides can lag behind implementation details.

## Skill directory

| Skill | Use it for |
| --- | --- |
| [ApplicationSecurityAndSupplyChain](./ApplicationSecurityAndSupplyChain/SKILL.md) | Security-sensitive application changes and dependency or supply-chain controls |
| [BenchmarkingAndPerformance](./BenchmarkingAndPerformance/SKILL.md) | Benchmark fixtures, baselines, performance investigations, and reports |
| [CloudflareWorkerDeployment](./CloudflareWorkerDeployment/SKILL.md) | Worker implementation, configuration, and deployment |
| [CommandLineInterface](./CommandLineInterface/SKILL.md) | CLI inputs, options, output, remote mode, and exit behavior |
| [DocumentationMaintenance](./DocumentationMaintenance/SKILL.md) | README, guides, examples, links, and changelog accuracy |
| [GoBackendDevelopment](./GoBackendDevelopment/SKILL.md) | Go API server and backend integration |
| [GitHubActionsAndCI](./GitHubActionsAndCI/SKILL.md) | GitHub Actions, CI, security automation, and repository automation |
| [GraphTheoryAndAlgorithms](./GraphTheoryAndAlgorithms/SKILL.md) | Graph algorithms and diagram-family-aware rules |
| [MermaidDiagramSpecification](./MermaidDiagramSpecification/SKILL.md) | Mermaid syntax, parser support, fixtures, and examples |
| [NodeJsAndJavaScript](./NodeJsAndJavaScript/SKILL.md) | Node parser bridge and JavaScript/TypeScript behavior |
| [RestApiDesign](./RestApiDesign/SKILL.md) | Go API routes, payloads, status codes, and OpenAPI contract |
| [StaticAnalysisAndLinting](./StaticAnalysisAndLinting/SKILL.md) | Rule registry, configuration, suppressions, and issue reporting |
| [SystemsProgrammingAndProcessManagement](./SystemsProgrammingAndProcessManagement/SKILL.md) | Parser subprocess lifecycle, worker protocol, bounds, and shutdown |

## Choosing between related skills

- For a graph-based lint rule, use [StaticAnalysisAndLinting](./StaticAnalysisAndLinting/SKILL.md) for registry/configuration and [GraphTheoryAndAlgorithms](./GraphTheoryAndAlgorithms/SKILL.md) for the algorithm.
- For an API change, use [RestApiDesign](./RestApiDesign/SKILL.md) with the implementation skill for the target runtime. The Go API and Worker contracts differ.
- For a workflow change, use [GitHubActionsAndCI](./GitHubActionsAndCI/SKILL.md); add [CloudflareWorkerDeployment](./CloudflareWorkerDeployment/SKILL.md) when the change deploys the Worker.
- For behavior changes that affect user-facing docs, pair the implementation skill with [DocumentationMaintenance](./DocumentationMaintenance/SKILL.md).
