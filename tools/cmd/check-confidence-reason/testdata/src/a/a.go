package a

import "github.com/hatyibei/shingan/domain"

// inlineOK sets the field inline — no diagnostic.
func inlineOK() domain.Finding {
	return domain.Finding{RuleName: "r", ConfidenceReason: "deterministic"}
}

// inlineMissing omits the field with no rescue — diagnostic.
func inlineMissing() domain.Finding {
	return domain.Finding{RuleName: "r", Message: "m"} // want `domain\.Finding literal missing ConfidenceReason`
}

// sentinelOK is the empty "no finding" sentinel — no diagnostic.
func sentinelOK() domain.Finding {
	return domain.Finding{}
}

// postAssignOK sets the field after construction — no diagnostic (the awk
// script could not see this).
func postAssignOK() domain.Finding {
	f := domain.Finding{RuleName: "r"}
	f.ConfidenceReason = "heuristic"
	return f
}

// postAssignMissing builds via a var but never sets the field — diagnostic.
func postAssignMissing() domain.Finding {
	f := domain.Finding{RuleName: "r"} // want `domain\.Finding literal missing ConfidenceReason`
	return f
}

// factoryOK is a factory that sets the field via assignment — no diagnostic.
func factoryOK(rule string) domain.Finding {
	out := domain.Finding{RuleName: rule}
	out.ConfidenceReason = "structural"
	return out
}

// sliceLiteralMissing constructs a Finding inside a slice with no rescue —
// diagnostic.
func sliceLiteralMissing() []domain.Finding {
	return []domain.Finding{
		{RuleName: "r", Message: "m"}, // want `domain\.Finding literal missing ConfidenceReason`
	}
}
