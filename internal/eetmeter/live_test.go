package eetmeter

import (
	"context"
	"os"
	"testing"
)

// TestLive_ListRecipes exercises the real Mijn Eetmeter API in token mode. It is
// skipped unless EETMETER_LIVE_TOKEN and EETMETER_LIVE_DEVICE_ID are set. It only
// reads.
//
//	EETMETER_LIVE_TOKEN=... EETMETER_LIVE_DEVICE_ID=... go test ./internal/eetmeter -run Live -v
func TestLive_ListRecipes(t *testing.T) {
	token := os.Getenv("EETMETER_LIVE_TOKEN")
	device := os.Getenv("EETMETER_LIVE_DEVICE_ID")
	if token == "" || device == "" {
		t.Skip("set EETMETER_LIVE_TOKEN and EETMETER_LIVE_DEVICE_ID for the live read test")
	}

	c := New("", "", Options{Token: token, DeviceID: device})

	if err := c.Login(context.Background()); err != nil {
		t.Fatalf("Login (should be a no-op in token mode): %v", err)
	}
	recipes, err := c.ListRecipes(context.Background())
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if len(recipes) == 0 {
		t.Fatal("expected at least one recipe")
	}
	for _, r := range recipes {
		if r.ID == "" || r.Name == "" {
			t.Fatalf("recipe missing Id/Name: %+v", r)
		}
		for _, it := range r.Items {
			if it.ProductUnitID == "" && !it.IsCustom {
				t.Fatalf("recipe %q ingredient %q has no ProductUnitId and is not custom", r.Name, it.ProductName)
			}
		}
	}
	t.Logf("parsed %d recipes; first: %q with %d ingredients",
		len(recipes), recipes[0].Name, len(recipes[0].Items))
}

// TestLive_WriteRoundTrip creates a throwaway recipe, updates it, and deletes it
// against the real API. Requires EETMETER_LIVE_TOKEN + EETMETER_LIVE_DEVICE_ID
// AND the explicit opt-in EETMETER_LIVE_WRITE=1. It cleans up after itself.
func TestLive_WriteRoundTrip(t *testing.T) {
	token := os.Getenv("EETMETER_LIVE_TOKEN")
	device := os.Getenv("EETMETER_LIVE_DEVICE_ID")
	if token == "" || device == "" || os.Getenv("EETMETER_LIVE_WRITE") != "1" {
		t.Skip("set EETMETER_LIVE_TOKEN, EETMETER_LIVE_DEVICE_ID and EETMETER_LIVE_WRITE=1")
	}
	ctx := context.Background()
	c := New("", "", Options{Token: token, DeviceID: device})

	src, err := c.ListRecipes(ctx)
	if err != nil || len(src) == 0 {
		t.Fatalf("need at least one source recipe: %v", err)
	}
	// borrow one real ingredient line so the productUnitId is valid
	tmpl := Recipe{
		Name:             "zzz-eetmeter-sync WRITE TEST (auto-deleted)",
		NumberOfPortions: 1,
		Items:            src[0].Items[:1],
	}

	created, err := c.CreateRecipe(ctx, tmpl)
	if err != nil {
		t.Fatalf("CreateRecipe: %v", err)
	}
	if created.ID == "" || created.NumberOfPortions != 1 {
		t.Fatalf("create result: %+v", created)
	}
	t.Cleanup(func() {
		if err := c.DeleteRecipe(context.Background(), created.ID); err != nil {
			t.Errorf("cleanup DeleteRecipe(%s): %v — delete it in the app", created.ID, err)
		}
	})

	created.NumberOfPortions = 3
	updated, err := c.UpdateRecipe(ctx, created.ID, created)
	if err != nil {
		t.Fatalf("UpdateRecipe: %v", err)
	}
	if updated.ID != created.ID || updated.NumberOfPortions != 3 {
		t.Fatalf("update result: %+v", updated)
	}

	if err := c.DeleteRecipe(ctx, created.ID); err != nil {
		t.Fatalf("DeleteRecipe: %v", err)
	}
	after, err := c.ListRecipes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range after {
		if r.ID == created.ID {
			t.Fatalf("recipe %s still in the list after delete", created.ID)
		}
	}
	t.Logf("create → update(portions=3) → delete round-trip OK")
}
