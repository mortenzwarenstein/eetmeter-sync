package syncengine

import (
	"testing"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/fingerprint"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/store"
)

// rec builds a one-ingredient recipe whose content is determined by amount.
func rec(id string, amount float64) *eetmeter.Recipe {
	return &eetmeter.Recipe{
		ID:               id,
		Name:             "R",
		NumberOfPortions: 2,
		Items: []eetmeter.Ingredient{
			{ID: "line-" + id, ProductUnitID: "u1", ProductName: "Rice", UnitName: "g", Amount: amount},
		},
	}
}

func hash(amount float64) []byte { return fingerprint.Of(*rec("", amount)).Bytes() }

func kindName(k DecisionKind) string {
	return [...]string{
		"Noop", "CreateInA", "CreateInB", "UpdateInA", "UpdateInB", "LinkOnly",
		"FlagConflict", "KeepConflict", "ApplyResolution", "Retire", "Skip",
	}[k]
}

func TestDecide(t *testing.T) {
	// A link where both sides were last synced with amount=100.
	syncedLink := func() *store.Link {
		return &store.Link{
			ID: 1, Name: "R", ARecipeID: "a1", BRecipeID: "b1",
			ABaselineHash: hash(100), BBaselineHash: hash(100), Status: store.StatusActive,
		}
	}

	tests := []struct {
		name   string
		in     Input
		want   DecisionKind
		winner string // for ApplyResolution
	}{
		// --- first sight, no link ---
		{"first sight: A only", Input{A: rec("a1", 100)}, CreateInB, ""},
		{"first sight: B only", Input{B: rec("b1", 100)}, CreateInA, ""},
		{"first sight: both equal", Input{A: rec("a1", 100), B: rec("b1", 100)}, LinkOnly, ""},
		{"first sight: both differ", Input{A: rec("a1", 100), B: rec("b1", 200)}, FlagConflict, ""},
		{"first sight: neither", Input{}, Noop, ""},

		// --- ambiguous names ---
		{"ambiguous in A", Input{A: rec("a1", 100), B: rec("b1", 100), AmbiguousA: true}, Skip, ""},
		{"ambiguous in B", Input{A: rec("a1", 100), B: rec("b1", 100), AmbiguousB: true}, Skip, ""},

		// --- retired link ---
		{"retired link is skipped", Input{
			Link: &store.Link{ID: 1, Name: "R", Status: store.StatusRetired, ARecipeID: "a1", BRecipeID: "b1"},
			A:    rec("a1", 100), B: rec("b1", 100),
		}, Skip, ""},

		// --- one-sided deletion ---
		{"A deleted", Input{Link: syncedLink(), A: nil, B: rec("b1", 100)}, Retire, ""},
		{"B deleted", Input{Link: syncedLink(), A: rec("a1", 100), B: nil}, Retire, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(tc.in)
			if got.Kind != tc.want {
				t.Fatalf("Decide() = %s, want %s (reason: %q)", kindName(got.Kind), kindName(tc.want), got.Reason)
			}
		})
	}
}

func TestDecide_ChangeDetection(t *testing.T) {
	base := &store.Link{
		ID: 1, Name: "R", ARecipeID: "a1", BRecipeID: "b1",
		ABaselineHash: hash(100), BBaselineHash: hash(100), Status: store.StatusActive,
	}

	tests := []struct {
		name string
		a, b *eetmeter.Recipe
		want DecisionKind
	}{
		{"neither changed", rec("a1", 100), rec("b1", 100), Noop},
		{"A changed only", rec("a1", 150), rec("b1", 100), UpdateInB},
		{"B changed only", rec("a1", 100), rec("b1", 150), UpdateInA},
		{"both changed, differ", rec("a1", 150), rec("b1", 175), FlagConflict},
		{"both changed, same", rec("a1", 150), rec("b1", 150), LinkOnly},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := Decide(Input{Link: base, A: tc.a, B: tc.b})
			if d.Kind != tc.want {
				t.Fatalf("Decide() = %s, want %s", kindName(d.Kind), kindName(tc.want))
			}
		})
	}
}

func TestDecide_Conflicts(t *testing.T) {
	conflict := &store.Link{
		ID: 1, Name: "R", ARecipeID: "a1", BRecipeID: "b1",
		ABaselineHash: hash(100), BBaselineHash: hash(100), Status: store.StatusConflict,
	}

	t.Run("still divergent -> KeepConflict", func(t *testing.T) {
		d := Decide(Input{Link: conflict, A: rec("a1", 150), B: rec("b1", 175)})
		if d.Kind != KeepConflict {
			t.Fatalf("got %s, want KeepConflict", kindName(d.Kind))
		}
	})

	t.Run("converged manually -> LinkOnly", func(t *testing.T) {
		d := Decide(Input{Link: conflict, A: rec("a1", 150), B: rec("b1", 150)})
		if d.Kind != LinkOnly {
			t.Fatalf("got %s, want LinkOnly", kindName(d.Kind))
		}
	})

	t.Run("pending resolution -> ApplyResolution", func(t *testing.T) {
		d := Decide(Input{Link: conflict, A: rec("a1", 150), B: rec("b1", 175), Pending: "a"})
		if d.Kind != ApplyResolution || d.Winner != "a" {
			t.Fatalf("got %s/%q, want ApplyResolution/a", kindName(d.Kind), d.Winner)
		}
	})

	t.Run("pending resolution on non-conflict link -> Skip (stale)", func(t *testing.T) {
		active := *conflict
		active.Status = store.StatusActive
		d := Decide(Input{Link: &active, A: rec("a1", 100), B: rec("b1", 100), Pending: "a"})
		if d.Kind != Skip {
			t.Fatalf("got %s, want Skip", kindName(d.Kind))
		}
	})
}
