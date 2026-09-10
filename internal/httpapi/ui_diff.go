package httpapi

import (
	"sort"
	"strconv"
)

func itoa(n int) string { return strconv.Itoa(n) }

// ftoa formats an ingredient amount without a trailing ".0" for whole numbers.
func ftoa(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// diffPair is an A/B comparison of one scalar field.
type diffPair struct {
	A, B    string
	Differs bool
}

// itemDiffRow is one ingredient line aligned across the two sides. Amount/UnitName
// are the A-side values when present, else the B-side values, so a template can
// show something for an OnlyB row too.
type itemDiffRow struct {
	ProductName   string
	AAmount       string
	AUnit         string
	BAmount       string
	BUnit         string
	OnlyA         bool
	OnlyB         bool
	AmountDiffers bool
	UnitDiffers   bool
}

// Differs reports whether the row is anything other than an identical line on
// both sides.
func (r itemDiffRow) Differs() bool {
	return r.OnlyA || r.OnlyB || r.AmountDiffers || r.UnitDiffers
}

// diffSides compares the two current versions of a conflicted recipe: the number
// of portions and the ingredient lines. Output ordering is deterministic (by
// product name) so rendering and tests are stable.
func diffSides(a, b *ConflictSide) (portions diffPair, items []itemDiffRow) {
	portions = diffPair{A: itoa(sideValue(a).NumberOfPortions), B: itoa(sideValue(b).NumberOfPortions)}
	portions.Differs = portions.A != portions.B

	aItems := indexItems(a)
	bItems := indexItems(b)

	keys := make([]string, 0, len(aItems)+len(bItems))
	seen := map[string]bool{}
	for k := range aItems {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range bItems {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)

	for _, k := range keys {
		ai, aok := aItems[k]
		bi, bok := bItems[k]
		row := itemDiffRow{}
		switch {
		case aok && bok:
			row.ProductName = ai.ProductName
			row.AAmount, row.AUnit = ftoa(ai.Amount), ai.UnitName
			row.BAmount, row.BUnit = ftoa(bi.Amount), bi.UnitName
			row.AmountDiffers = ai.Amount != bi.Amount
			row.UnitDiffers = ai.UnitName != bi.UnitName
		case aok:
			row.ProductName = ai.ProductName
			row.AAmount, row.AUnit = ftoa(ai.Amount), ai.UnitName
			row.OnlyA = true
		default:
			row.ProductName = bi.ProductName
			row.BAmount, row.BUnit = ftoa(bi.Amount), bi.UnitName
			row.OnlyB = true
		}
		items = append(items, row)
	}
	return portions, items
}

func sideValue(s *ConflictSide) ConflictSide {
	if s == nil {
		return ConflictSide{}
	}
	return *s
}

// indexItems keys a side's ingredient lines by product name, falling back to the
// product-unit id when a name is duplicated within the side.
func indexItems(s *ConflictSide) map[string]ConflictItem {
	out := map[string]ConflictItem{}
	if s == nil {
		return out
	}
	for _, it := range s.Items {
		key := it.ProductName
		if _, clash := out[key]; clash {
			key = it.ProductName + "\x00" + it.ProductUnitID
		}
		out[key] = it
	}
	return out
}
