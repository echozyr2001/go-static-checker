// Package analysis builds call models and evaluates declarative contracts.
package analysis

// Location identifies a physical source position.
type Location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// Call records one statically resolved edge or an explicit analysis gap.
type Call struct {
	Target    string   `json:"target"`
	Symbol    string   `json:"symbol"`
	Location  Location `json:"location"`
	Unknown   string   `json:"unknown,omitempty"`
	Primitive bool     `json:"-"`
}

// Function represents a body independently of the Go front end.
type Function struct {
	ID       string
	Symbol   string
	Location Location
	Calls    []Call
	Entry    bool
}

// Program holds function bodies and their call edges.
type Program struct {
	Functions map[string]Function
}

// Policy specifies entry symbols, prohibited APIs and external assumptions.
type Policy struct {
	RuleID    string
	Entries   []string
	Forbidden []string
	Trusted   []string
	OnUnknown string
}

// Evidence records a prohibited edge or an unresolved part of a call chain.
type Evidence struct {
	Code   string `json:"code"`
	Reason string `json:"reason"`
	Chain  []Call `json:"chain"`
}

// Assumption identifies an explicitly trusted external function.
type Assumption struct {
	Symbol   string   `json:"symbol"`
	Location Location `json:"location"`
}

// Result distinguishes violations, satisfied contracts and incomplete analysis.
type Result struct {
	RuleID      string       `json:"rule_id"`
	Entry       string       `json:"entry"`
	Status      string       `json:"status"`
	Complete    bool         `json:"complete"`
	OnUnknown   string       `json:"on_unknown"`
	Location    Location     `json:"location"`
	Assumptions []Assumption `json:"assumptions"`
	Evidence    []Evidence   `json:"evidence"`
}
