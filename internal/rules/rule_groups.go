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

// SequenceRules returns currently supported sequence diagram rules.
func SequenceRules() []Rule {
	return []Rule{
		SequenceNoUndefinedActors{},
		SequenceNoDuplicateActors{},
		SequenceMaxNesting{},
	}
}

// ClassRules returns currently supported class diagram rules.
func ClassRules() []Rule {
	return []Rule{
		ClassNoCircularInheritance{},
		ClassNoDuplicateClasses{},
		ClassMaxInheritanceDepth{},
	}
}

// ERRules is a placeholder for incremental ER diagram support.
func ERRules() []Rule { return []Rule{} }

// StateRules is a placeholder for incremental state diagram support.
func StateRules() []Rule { return []Rule{} }
