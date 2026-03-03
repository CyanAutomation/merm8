package sarif

import "github.com/CyanAutomation/merm8/internal/model"

// RequestMetadata captures request-level details embedded in SARIF output.
type RequestMetadata struct {
	RequestURI  string
	ArtifactURI string
}

type Report struct {
	Version string `json:"version"`
	Schema  string `json:"$schema"`
	Runs    []Run  `json:"runs"`
}

type Run struct {
	Tool            Tool            `json:"tool"`
	Artifacts       []Artifact      `json:"artifacts,omitempty"`
	Invocations     []Invocation    `json:"invocations,omitempty"`
	OriginalUriBase map[string]Base `json:"originalUriBaseIds,omitempty"`
	Results         []Result        `json:"results"`
}

type Tool struct {
	Driver Driver `json:"driver"`
}

type Driver struct {
	Name           string `json:"name"`
	InformationURI string `json:"informationUri,omitempty"`
	Rules          []Rule `json:"rules,omitempty"`
}

type Rule struct {
	ID               string      `json:"id"`
	Name             string      `json:"name,omitempty"`
	ShortDescription MessageText `json:"shortDescription,omitempty"`
}

type MessageText struct {
	Text string `json:"text"`
}

type Artifact struct {
	Location ArtifactLocation `json:"location"`
}

type ArtifactLocation struct {
	URI string `json:"uri"`
}

type Invocation struct {
	ExecutionSuccessful bool              `json:"executionSuccessful"`
	Properties          map[string]string `json:"properties,omitempty"`
}

type Base struct {
	URI string `json:"uri"`
}

type Result struct {
	RuleID    string      `json:"ruleId"`
	Level     string      `json:"level"`
	Message   MessageText `json:"message"`
	Locations []Location  `json:"locations,omitempty"`
}

type Location struct {
	PhysicalLocation PhysicalLocation `json:"physicalLocation"`
}

type PhysicalLocation struct {
	ArtifactLocation ArtifactLocation `json:"artifactLocation"`
	Region           *Region          `json:"region,omitempty"`
}

type Region struct {
	StartLine   int `json:"startLine,omitempty"`
	StartColumn int `json:"startColumn,omitempty"`
}

// Transform converts lint issues and request metadata into SARIF 2.1.0.
func Transform(issues []model.Issue, meta RequestMetadata) Report {
	if meta.ArtifactURI == "" {
		meta.ArtifactURI = "request://mermaid"
	}
	rules := make([]Rule, 0, len(issues))
	ruleSeen := map[string]struct{}{}
	results := make([]Result, 0, len(issues))
	for _, issue := range issues {
		if _, ok := ruleSeen[issue.RuleID]; !ok {
			ruleSeen[issue.RuleID] = struct{}{}
			rules = append(rules, Rule{ID: issue.RuleID, Name: issue.RuleID})
		}
		result := Result{
			RuleID:  issue.RuleID,
			Level:   MapSeverityToLevel(issue.Severity),
			Message: MessageText{Text: issue.Message},
		}
		if issue.Line != nil || issue.Column != nil {
			region := &Region{}
			if issue.Line != nil {
				region.StartLine = *issue.Line
			}
			if issue.Column != nil {
				region.StartColumn = *issue.Column
			}
			result.Locations = []Location{{
				PhysicalLocation: PhysicalLocation{
					ArtifactLocation: ArtifactLocation{URI: meta.ArtifactURI},
					Region:           region,
				},
			}}
		}
		results = append(results, result)
	}

	run := Run{
		Tool: Tool{Driver: Driver{
			Name:           "merm8",
			InformationURI: "https://github.com/CyanAutomation/merm8",
			Rules:          rules,
		}},
		Artifacts: []Artifact{{Location: ArtifactLocation{URI: meta.ArtifactURI}}},
		Results:   results,
	}
	if meta.RequestURI != "" {
		run.Invocations = []Invocation{{
			ExecutionSuccessful: true,
			Properties: map[string]string{
				"request-uri": meta.RequestURI,
			},
		}}
	}
	return Report{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs:    []Run{run},
	}
}
