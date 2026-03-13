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

// ERRules returns currently supported ER diagram rules.
func ERRules() []Rule {
	return []Rule{
		ERNoCircularChain{},
		ERNoSelfReferential{},
	}
}

// StateRules returns currently supported state diagram rules.
func StateRules() []Rule {
	return []Rule{
		StateNoCircularTransitions{},
		StateNoUnreachable{},
		StateMaxTransitions{},
	}
}
