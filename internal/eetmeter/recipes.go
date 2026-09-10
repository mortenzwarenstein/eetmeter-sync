package eetmeter

import (
	"context"
	"net/http"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/uid"
)

// ListRecipes returns every recipe ("combined product") in the account.
//
// CONFIRMED: GET combinedproduct returns the complete set in one response, no
// pagination.
func (c *Client) ListRecipes(ctx context.Context) ([]Recipe, error) {
	var list recipeList
	if err := c.do(ctx, http.MethodGet, "combinedproduct", nil, &list); err != nil {
		return nil, err
	}
	return list.Items, nil
}

// GetRecipe fetches one recipe by id.
func (c *Client) GetRecipe(ctx context.Context, id string) (Recipe, error) {
	var r Recipe
	if err := c.do(ctx, http.MethodGet, "combinedproduct/"+id, nil, &r); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

// CreateRecipe creates a recipe in the account. The recipe id is generated here
// (the API honours a client-supplied id); item ids are generated too but the
// server re-assigns them. Returns the recipe as stored by the server.
//
// CONFIRMED: PUT combinedproduct (collection), camelCase body, minimal item
// fields. The extra item fields sent for fidelity are UNVERIFIED — see
// recipeWrite.
func (c *Client) CreateRecipe(ctx context.Context, r Recipe) (Recipe, error) {
	return c.putRecipe(ctx, uid.New(), r)
}

// UpdateRecipe replaces the content of an existing recipe (same PUT endpoint,
// with the existing id).
//
// UNVERIFIED: assumes PUT combinedproduct with an existing id upserts. The spike
// confirms; the delete-then-recreate fallback (receiving copy of a linked pair
// only, permitted by FR-010) is used if it does not.
func (c *Client) UpdateRecipe(ctx context.Context, id string, r Recipe) (Recipe, error) {
	return c.putRecipe(ctx, id, r)
}

func (c *Client) putRecipe(ctx context.Context, id string, r Recipe) (Recipe, error) {
	body := r.toWrite(id, uid.New)
	var saved Recipe
	if err := c.do(ctx, http.MethodPut, "combinedproduct", body, &saved); err != nil {
		return Recipe{}, err
	}
	if saved.ID != "" {
		return saved, nil
	}
	// Fall back to a direct fetch by the id we chose.
	return c.GetRecipe(ctx, id)
}

// DeleteRecipe removes a recipe. Not used by the sync (deletions are never
// propagated) but available for spike cleanup.
//
// UNVERIFIED: method/path.
func (c *Client) DeleteRecipe(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "combinedproduct/"+id, nil, nil)
}
