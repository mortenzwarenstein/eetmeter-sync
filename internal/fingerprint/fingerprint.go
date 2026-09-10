// Package fingerprint reduces a recipe to a stable hash of its portable content,
// so a sync can tell whether either account's copy changed since the last run
// without relying on a modification timestamp (the API exposes none).
package fingerprint

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"sort"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
)

// Sum is a recipe content hash.
type Sum [32]byte

// Bytes returns the raw hash, suitable for a bytea column.
func (s Sum) Bytes() []byte { return s[:] }

// String returns the hex encoding.
func (s Sum) String() string { return hex.EncodeToString(s[:]) }

// FromBytes rebuilds a Sum from a stored hash. A nil/short slice yields the zero
// Sum, which never equals a real fingerprint.
func FromBytes(b []byte) Sum {
	var s Sum
	if len(b) == len(s) {
		copy(s[:], b)
	}
	return s
}

// canonRecipe is the normalized shape that gets hashed — only the content a
// write to another account can actually reproduce. Excluded, and why:
//   - recipe Name: it is the match key (equal by construction for a linked pair)
//   - all server-assigned IDs (recipe Id, item Id, CombinedProductId): per-account
//   - ProductName / UnitName / BrandName: derived by the server from ProductUnitId
//   - PreparationMethodName: the API ignores it on write (spike-confirmed), so it
//     must not drive sync decisions or a recipe with a non-default method would
//     never converge
//   - OwnProductUnitId: a per-account custom-unit id
type canonRecipe struct {
	NumberOfPortions int
	Items            []canonItem
}

type canonItem struct {
	ProductUnitID        string
	BrandProductID       string
	BaseProductSynonymID string
	IsCustom             bool
	// AmountMilli is Amount rounded to 3 decimals, as an integer, so float
	// formatting noise does not register as a change.
	AmountMilli int64
}

// Of computes the fingerprint of a recipe.
func Of(r eetmeter.Recipe) Sum {
	c := canonRecipe{NumberOfPortions: r.NumberOfPortions}
	c.Items = make([]canonItem, 0, len(r.Items))
	for _, it := range r.Items {
		c.Items = append(c.Items, canonItem{
			ProductUnitID:        it.ProductUnitID,
			BrandProductID:       it.BrandProductID,
			BaseProductSynonymID: it.BaseProductSynonymID,
			IsCustom:             it.IsCustom,
			AmountMilli:          int64(math.Round(it.Amount * 1000)),
		})
	}
	sort.Slice(c.Items, func(i, j int) bool {
		a, b := c.Items[i], c.Items[j]
		switch {
		case a.ProductUnitID != b.ProductUnitID:
			return a.ProductUnitID < b.ProductUnitID
		case a.BrandProductID != b.BrandProductID:
			return a.BrandProductID < b.BrandProductID
		case a.BaseProductSynonymID != b.BaseProductSynonymID:
			return a.BaseProductSynonymID < b.BaseProductSynonymID
		default:
			return a.AmountMilli < b.AmountMilli
		}
	})

	// json.Marshal of a struct is deterministic (field declaration order, no map
	// iteration involved here), which is all the canonical encoding this needs.
	b, err := json.Marshal(c)
	if err != nil {
		panic("fingerprint: marshal canonical recipe: " + err.Error())
	}
	return sha256.Sum256(b)
}

// Equal reports whether a raw stored hash matches a freshly computed one.
func Equal(stored []byte, computed Sum) bool {
	return FromBytes(stored) == computed
}
