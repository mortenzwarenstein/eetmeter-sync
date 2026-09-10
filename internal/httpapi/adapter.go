package httpapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/store"
)

// StoreAdapter adapts *store.Store to the narrow Store interface the handlers
// use, translating persistence types into API types.
type StoreAdapter struct{ S *store.Store }

// NewStore wraps a *store.Store for use by the HTTP server.
func NewStore(s *store.Store) Store { return StoreAdapter{S: s} }

// Ping reports database reachability.
func (a StoreAdapter) Ping(ctx context.Context) error { return a.S.Ping(ctx) }

// LatestRunSummary returns the raw summary JSON of the most recent run.
func (a StoreAdapter) LatestRunSummary(ctx context.Context) ([]byte, bool, error) {
	run, err := a.S.LatestRun(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return run.Summary, true, nil
}

// ListConflicts returns the unresolved conflicts with both captured versions.
func (a StoreAdapter) ListConflicts(ctx context.Context) ([]Conflict, error) {
	rows, err := a.S.ListConflicts(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Conflict, 0, len(rows))
	for _, r := range rows {
		out = append(out, Conflict{
			LinkID:            r.LinkID,
			Name:              r.Name,
			DetectedAt:        r.DetectedAt,
			A:                 conflictSide(r.ARecipeID, r.ASnapshot),
			B:                 conflictSide(r.BRecipeID, r.BSnapshot),
			PendingResolution: r.Pending,
		})
	}
	return out, nil
}

// RecordResolution stores a pending resolution; found is false if the link is
// not currently in conflict.
func (a StoreAdapter) RecordResolution(ctx context.Context, linkID int64, winner string) (bool, error) {
	err := a.S.UpsertResolution(ctx, linkID, winner)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func conflictSide(recipeID string, snapshot []byte) *ConflictSide {
	if len(snapshot) == 0 {
		if recipeID == "" {
			return nil
		}
		return &ConflictSide{RecipeID: recipeID, Items: []ConflictItem{}}
	}
	var r eetmeter.Recipe
	if err := json.Unmarshal(snapshot, &r); err != nil {
		return &ConflictSide{RecipeID: recipeID, Items: []ConflictItem{}}
	}
	side := &ConflictSide{
		RecipeID:         recipeID,
		NumberOfPortions: r.NumberOfPortions,
		Items:            make([]ConflictItem, 0, len(r.Items)),
	}
	for _, it := range r.Items {
		side.Items = append(side.Items, ConflictItem{
			ProductName:    it.ProductName,
			UnitName:       it.UnitName,
			Amount:         it.Amount,
			ProductUnitID:  it.ProductUnitID,
			BrandProductID: it.BrandProductID,
		})
	}
	return side
}
