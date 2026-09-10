package eetmeter

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// captured PUT body (camelCase write format)
type putBody struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	NumberOfPortions int    `json:"numberOfPortions"`
	Items            []struct {
		ID                    string  `json:"id"`
		ProductUnitID         string  `json:"productUnitId"`
		Amount                float64 `json:"amount"`
		PreparationMethodName string  `json:"preparationMethodName"`
	} `json:"items"`
}

func TestCreateRecipe_PutsCamelCaseBodyWithGeneratedIDs(t *testing.T) {
	var got putBody
	var gotMethod, gotPath string
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/account/credentials" {
			w.Write([]byte(`{"Token":"T"}`))
			return
		}
		gotMethod, gotPath = r.Method, r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&got)
		// echo back in PascalCase read format
		json.NewEncoder(w).Encode(Recipe{
			ID: got.ID, Name: got.Name, NumberOfPortions: got.NumberOfPortions,
			Items: []Ingredient{{ID: "server-line-id", ProductUnitID: got.Items[0].ProductUnitID, Amount: got.Items[0].Amount}},
		})
	})
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}

	in := Recipe{
		ID:               "SOURCE-ID",
		Name:             "Curry",
		NumberOfPortions: 4,
		Items: []Ingredient{{
			ID: "SOURCE-LINE", ProductUnitID: "u1", ProductName: "Kokosmelk",
			UnitName: "ml", Amount: 400, PreparationMethodName: "Rauw",
		}},
	}
	out, err := c.CreateRecipe(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}

	if gotMethod != http.MethodPut || gotPath != "/api/combinedproduct" {
		t.Fatalf("create hit %s %s, want PUT /api/combinedproduct", gotMethod, gotPath)
	}
	if got.ID == "" || got.ID == "SOURCE-ID" {
		t.Fatalf("recipe id should be freshly generated, got %q", got.ID)
	}
	if len(got.Items) != 1 || got.Items[0].ID == "" || got.Items[0].ID == "SOURCE-LINE" {
		t.Fatalf("item id should be freshly generated, got %+v", got.Items)
	}
	if got.Items[0].ProductUnitID != "u1" || got.Items[0].Amount != 400 {
		t.Fatalf("ingredient reference/amount not carried: %+v", got.Items[0])
	}
	if got.Items[0].PreparationMethodName != "Rauw" {
		t.Fatalf("preparation method not carried for fidelity: %q", got.Items[0].PreparationMethodName)
	}
	if out.ID != got.ID {
		t.Fatalf("returned id %q != sent id %q", out.ID, got.ID)
	}
}

func TestUpdateRecipe_PutsWithExistingID(t *testing.T) {
	var got putBody
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/account/credentials" {
			w.Write([]byte(`{"Token":"T"}`))
			return
		}
		if r.Method != http.MethodPut || r.URL.Path != "/api/combinedproduct" {
			t.Errorf("update hit %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		json.NewEncoder(w).Encode(Recipe{ID: got.ID, Name: got.Name, NumberOfPortions: got.NumberOfPortions})
	})
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	out, err := c.UpdateRecipe(context.Background(), "existing-id", Recipe{Name: "Curry", NumberOfPortions: 6})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "existing-id" {
		t.Fatalf("update should keep the id, sent %q", got.ID)
	}
	if out.NumberOfPortions != 6 {
		t.Fatalf("update result = %+v", out)
	}
}

func TestCreateRecipe_FallsBackToGetWhenNoIDInResponse(t *testing.T) {
	c := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/account/credentials":
			w.Write([]byte(`{"Token":"T"}`))
		case r.Method == http.MethodPut:
			w.Write([]byte(`{}`)) // no id echoed
		default: // GET /api/combinedproduct/<id>
			w.Write([]byte(`{"Id":"` + pathID(r.URL.Path) + `","Name":"Curry","NumberOfPortions":4,"Items":[]}`))
		}
	})
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	out, err := c.CreateRecipe(context.Background(), Recipe{Name: "Curry"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID == "" || out.Name != "Curry" {
		t.Fatalf("fallback GET result = %+v", out)
	}
}

func pathID(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}
