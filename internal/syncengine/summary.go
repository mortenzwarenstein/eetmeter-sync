package syncengine

import "time"

// Action names for a summary item. These are the values the HTTP API exposes.
const (
	ActionCreatedInA        = "createdInA"
	ActionCreatedInB        = "createdInB"
	ActionUpdatedInA        = "updatedInA"
	ActionUpdatedInB        = "updatedInB"
	ActionLinked            = "linked"
	ActionFlaggedConflict   = "flaggedConflict"
	ActionResolutionApplied = "resolutionApplied"
	ActionLinkRetired       = "linkRetired"
	ActionSkipped           = "skipped"
	ActionNoop              = "noop"
)

// Summary is the machine-readable outcome of one run. It is stored as the
// sync_run.summary jsonb and served verbatim by GET /sync/last.
type Summary struct {
	RunID      string             `json:"runId"`
	Trigger    string             `json:"trigger"`
	DryRun     bool               `json:"dryRun,omitempty"` // true: nothing was written, this is a preview
	Result     string             `json:"result"`           // running | succeeded | failed
	Error      string             `json:"error,omitempty"`
	StartedAt  time.Time          `json:"startedAt"`
	FinishedAt *time.Time         `json:"finishedAt,omitempty"`
	Accounts   map[string]AcctRef `json:"accounts"`
	Counts     Counts             `json:"counts"`
	Items      []Item             `json:"items"`
}

// AcctRef is the label shown for an account in a summary.
type AcctRef struct {
	Label string `json:"label"`
}

// Counts is the per-run tally, derived from Items.
type Counts struct {
	CreatedInA         int `json:"createdInA"`
	CreatedInB         int `json:"createdInB"`
	UpdatedInA         int `json:"updatedInA"`
	UpdatedInB         int `json:"updatedInB"`
	Linked             int `json:"linked"`
	FlaggedConflicts   int `json:"flaggedConflicts"`
	ResolutionsApplied int `json:"resolutionsApplied"`
	LinksRetired       int `json:"linksRetired"`
	Skipped            int `json:"skipped"`
	Noop               int `json:"noop"`
}

// Item is one recipe name's outcome.
type Item struct {
	Name   string `json:"name"`
	Action string `json:"action"`
	Detail string `json:"detail,omitempty"`
}

func newSummary(runID, trigger, labelA, labelB string, startedAt time.Time) *Summary {
	return &Summary{
		RunID:     runID,
		Trigger:   trigger,
		Result:    "running",
		StartedAt: startedAt,
		Accounts: map[string]AcctRef{
			"a": {Label: labelA},
			"b": {Label: labelB},
		},
		Items: []Item{},
	}
}

func (s *Summary) add(name, action, detail string) {
	s.Items = append(s.Items, Item{Name: name, Action: action, Detail: detail})
	switch action {
	case ActionCreatedInA:
		s.Counts.CreatedInA++
	case ActionCreatedInB:
		s.Counts.CreatedInB++
	case ActionUpdatedInA:
		s.Counts.UpdatedInA++
	case ActionUpdatedInB:
		s.Counts.UpdatedInB++
	case ActionLinked:
		s.Counts.Linked++
	case ActionFlaggedConflict:
		s.Counts.FlaggedConflicts++
	case ActionResolutionApplied:
		s.Counts.ResolutionsApplied++
	case ActionLinkRetired:
		s.Counts.LinksRetired++
	case ActionSkipped:
		s.Counts.Skipped++
	case ActionNoop:
		s.Counts.Noop++
	}
}

func (s *Summary) finish(result, errMsg string, at time.Time) {
	s.Result = result
	s.Error = errMsg
	s.FinishedAt = &at
}
