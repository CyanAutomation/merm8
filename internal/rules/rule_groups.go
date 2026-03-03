package rules

// FlowchartRules returns currently supported flowchart rules.
func FlowchartRules() []Rule {
	return []Rule{
		NoDuplicateNodeIDs{},
		NoDisconnectedNodes{},
		MaxFanout{},
		NoCycles{},
		MaxDepth{},
	}
}

// SequenceRules is a placeholder for incremental sequence diagram support.
func SequenceRules() []Rule { return []Rule{} }

// ClassRules is a placeholder for incremental class diagram support.
func ClassRules() []Rule { return []Rule{} }

// ERRules is a placeholder for incremental ER diagram support.
func ERRules() []Rule { return []Rule{} }

// StateRules is a placeholder for incremental state diagram support.
func StateRules() []Rule { return []Rule{} }
