// Package eetmeter is a client for the private "Mijn Eetmeter" API used by the
// Voedingscentrum mobile app. Only the pieces this project needs are
// implemented: authentication and CRUD for "combined products" (recipes).
//
// All paths used here — login, list, get, create/update (PUT combinedproduct),
// delete — are CONFIRMED against the live API. See the "Spike log" in
// specs/001-recipe-sync/contracts/eetmeter-api.md.
package eetmeter

// Recipe is a "combined product" (samengesteld product) — a named dish made of
// several ingredients. ID is assigned by the server and is unique per account,
// so it is never copied between accounts.
//
// Wire format is PascalCase (confirmed).
type Recipe struct {
	ID               string       `json:"Id,omitempty"`
	Name             string       `json:"Name"`
	NumberOfPortions int          `json:"NumberOfPortions"`
	Items            []Ingredient `json:"Items"`
	WebAccountID     string       `json:"WebAccountId,omitempty"`
}

// Ingredient is one line of a Recipe. ProductUnitID / BrandProductID /
// BaseProductSynonymID reference the shared Voedingscentrum food database and are
// copied verbatim when a recipe is propagated. Id and CombinedProductId are
// per-account server values. The API sends JSON null for the unset string
// fields; Go decodes that to "", which is all this code needs to distinguish.
type Ingredient struct {
	ID                    string  `json:"Id,omitempty"`
	CombinedProductID     string  `json:"CombinedProductId,omitempty"`
	Amount                float64 `json:"Amount"`
	ProductName           string  `json:"ProductName"`
	UnitName              string  `json:"UnitName"`
	ProductUnitID         string  `json:"ProductUnitId"`
	BrandProductID        string  `json:"BrandProductId"`
	BrandName             string  `json:"BrandName"`
	BaseProductSynonymID  string  `json:"BaseProductSynonymId"`
	OwnProductUnitID      string  `json:"OwnProductUnitId"`
	PreparationMethodName string  `json:"PreparationMethodName"`
	IsCustom              bool    `json:"IsCustom"`
}

// recipeList is the envelope returned by GET combinedproduct.
type recipeList struct {
	Items []Recipe `json:"Items"`
}

// recipeWrite is the body of PUT combinedproduct (create and update). The write
// format is camelCase (the read format is PascalCase). The recipe id is
// client-generated and honoured; item ids are sent but the server re-assigns
// them. preparationMethodName is sent but the server ignores it (spike-confirmed)
// — it is kept out of the fingerprint for that reason.
type recipeWrite struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	NumberOfPortions int               `json:"numberOfPortions"`
	Items            []ingredientWrite `json:"items"`
}

type ingredientWrite struct {
	ID                    string  `json:"id"`
	ProductUnitID         string  `json:"productUnitId"`
	Amount                float64 `json:"amount"`
	BrandProductID        string  `json:"brandProductId,omitempty"`
	BaseProductSynonymID  string  `json:"baseProductSynonymId,omitempty"`
	OwnProductUnitID      string  `json:"ownProductUnitId,omitempty"`
	PreparationMethodName string  `json:"preparationMethodName,omitempty"`
	IsCustom              bool    `json:"isCustom,omitempty"`
}

func (r Recipe) toWrite(id string, newItemID func() string) recipeWrite {
	w := recipeWrite{ID: id, Name: r.Name, NumberOfPortions: r.NumberOfPortions}
	w.Items = make([]ingredientWrite, len(r.Items))
	for i, it := range r.Items {
		w.Items[i] = ingredientWrite{
			ID:                    newItemID(),
			ProductUnitID:         it.ProductUnitID,
			Amount:                it.Amount,
			BrandProductID:        it.BrandProductID,
			BaseProductSynonymID:  it.BaseProductSynonymID,
			OwnProductUnitID:      it.OwnProductUnitID,
			PreparationMethodName: it.PreparationMethodName,
			IsCustom:              it.IsCustom,
		}
	}
	return w
}

// loginRequest is the body of POST account/credentials. Field names are
// camelCase (confirmed).
type loginRequest struct {
	DeviceID     string `json:"deviceId"`
	EmailAddress string `json:"emailAddress"`
	Password     string `json:"password"`
}

// loginResponse is the (partial) Account object returned by
// POST account/credentials. Only Token is used.
type loginResponse struct {
	Token        string `json:"Token"`
	EmailAddress string `json:"EmailAddress"`
}
