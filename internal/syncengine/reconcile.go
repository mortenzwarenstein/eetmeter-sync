// Package syncengine reconciles the recipe collections of two Mijn Eetmeter
// accounts. reconcile.go holds the pure decision function; engine.go performs
// the resulting reads and writes.
package syncengine

import (
	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/fingerprint"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/store"
)

// DecisionKind is what to do with one recipe name this run.
type DecisionKind int

const (
	// Noop: both sides already agree; nothing to do.
	Noop DecisionKind = iota
	// CreateInA: the recipe exists only in B; copy it into A.
	CreateInA
	// CreateInB: the recipe exists only in A; copy it into B.
	CreateInB
	// UpdateInA: a linked recipe changed in B only; make A match B.
	UpdateInA
	// UpdateInB: a linked recipe changed in A only; make B match A.
	UpdateInB
	// LinkOnly: both sides exist and already have equal content; record/refresh
	// the link with no API write.
	LinkOnly
	// FlagConflict: both sides changed and differ; write nothing, mark conflict.
	FlagConflict
	// KeepConflict: an existing conflict is still unresolved and still divergent.
	KeepConflict
	// ApplyResolution: a resolution was recorded; push Winner's content across.
	ApplyResolution
	// Retire: a linked recipe vanished from one side; stop syncing this pair.
	Retire
	// Skip: cannot act safely (ambiguous name, or a stale resolution).
	Skip
)

// Decision is the output of Decide for one recipe name.
type Decision struct {
	Kind   DecisionKind
	Winner string // "a" or "b", only for ApplyResolution
	Reason string // human-readable detail for Skip / conflicts / retire
}

// Input is everything Decide needs about one recipe name.
type Input struct {
	Link       *store.Link      // nil if no link exists yet
	A          *eetmeter.Recipe // nil if the recipe is absent in account A this run
	B          *eetmeter.Recipe // nil if absent in account B
	Pending    string           // pending resolution winner ("", "a", "b")
	AmbiguousA bool             // account A has more than one recipe with this name
	AmbiguousB bool             // account B has more than one recipe with this name
}

// Decide chooses what to do with one recipe name. It is pure: no I/O, no clock.
func Decide(in Input) Decision {
	if in.AmbiguousA {
		return Decision{Kind: Skip, Reason: "duplicate name in account A"}
	}
	if in.AmbiguousB {
		return Decision{Kind: Skip, Reason: "duplicate name in account B"}
	}

	// No link yet: first time this name is seen.
	if in.Link == nil {
		switch {
		case in.A != nil && in.B != nil:
			if fpEqual(in.A, in.B) {
				return Decision{Kind: LinkOnly}
			}
			return Decision{Kind: FlagConflict, Reason: "same name created independently on both sides with different content"}
		case in.A != nil:
			return Decision{Kind: CreateInB}
		case in.B != nil:
			return Decision{Kind: CreateInA}
		default:
			return Decision{Kind: Noop}
		}
	}

	l := in.Link

	if l.Status == store.StatusRetired {
		return Decision{Kind: Skip, Reason: "link retired after a one-sided deletion; delete the retired link to resume"}
	}

	// A side that was linked has disappeared -> retire (deletions never propagate).
	if (l.ARecipeID != "" && in.A == nil) || (l.BRecipeID != "" && in.B == nil) {
		return Decision{Kind: Retire, Reason: "recipe deleted on one side"}
	}

	// Link exists but a side was never populated and still is not present.
	if in.A == nil && in.B != nil {
		return Decision{Kind: CreateInA}
	}
	if in.B == nil && in.A != nil {
		return Decision{Kind: CreateInB}
	}
	if in.A == nil || in.B == nil {
		return Decision{Kind: Noop}
	}

	if in.Pending != "" {
		if l.Status == store.StatusConflict {
			return Decision{Kind: ApplyResolution, Winner: in.Pending}
		}
		return Decision{Kind: Skip, Reason: "stale resolution discarded (link no longer in conflict)"}
	}

	fa := fingerprint.Of(*in.A)
	fb := fingerprint.Of(*in.B)

	if l.Status == store.StatusConflict {
		if fa == fb {
			return Decision{Kind: LinkOnly} // converged manually
		}
		return Decision{Kind: KeepConflict, Reason: "still changed on both sides"}
	}

	changedA := !fingerprint.Equal(l.ABaselineHash, fa)
	changedB := !fingerprint.Equal(l.BBaselineHash, fb)

	switch {
	case !changedA && !changedB:
		return Decision{Kind: Noop}
	case changedA && !changedB:
		return Decision{Kind: UpdateInB}
	case !changedA && changedB:
		return Decision{Kind: UpdateInA}
	default: // both changed
		if fa == fb {
			return Decision{Kind: LinkOnly} // both edited to the same content
		}
		return Decision{Kind: FlagConflict, Reason: "changed on both sides since the last sync"}
	}
}

func fpEqual(a, b *eetmeter.Recipe) bool {
	return fingerprint.Of(*a) == fingerprint.Of(*b)
}
