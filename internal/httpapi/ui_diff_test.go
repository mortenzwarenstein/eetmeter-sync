package httpapi

import "testing"

func side(portions int, items ...ConflictItem) *ConflictSide {
	return &ConflictSide{NumberOfPortions: portions, Items: items}
}

func item(name string, amount float64, unit string) ConflictItem {
	return ConflictItem{ProductName: name, Amount: amount, UnitName: unit, ProductUnitID: name + "-u"}
}

func TestDiffSides(t *testing.T) {
	t.Run("identical sides produce no marks", func(t *testing.T) {
		a := side(4, item("Rice", 200, "g"), item("Water", 1, "l"))
		b := side(4, item("Rice", 200, "g"), item("Water", 1, "l"))
		portions, items := diffSides(a, b)
		if portions.Differs {
			t.Fatalf("portions marked: %+v", portions)
		}
		for _, r := range items {
			if r.Differs() {
				t.Fatalf("row marked: %+v", r)
			}
		}
	})

	t.Run("portion difference", func(t *testing.T) {
		portions, _ := diffSides(side(4), side(3))
		if !portions.Differs || portions.A != "4" || portions.B != "3" {
			t.Fatalf("portions = %+v", portions)
		}
	})

	t.Run("amount difference", func(t *testing.T) {
		_, items := diffSides(side(4, item("Rice", 200, "g")), side(4, item("Rice", 250, "g")))
		if len(items) != 1 || !items[0].AmountDiffers || items[0].UnitDiffers {
			t.Fatalf("items = %+v", items)
		}
	})

	t.Run("unit difference", func(t *testing.T) {
		_, items := diffSides(side(4, item("Milk", 1, "l")), side(4, item("Milk", 1, "dl")))
		if len(items) != 1 || items[0].AmountDiffers || !items[0].UnitDiffers {
			t.Fatalf("items = %+v", items)
		}
	})

	t.Run("item only in A", func(t *testing.T) {
		_, items := diffSides(side(4, item("Rice", 200, "g"), item("Salt", 5, "g")), side(4, item("Rice", 200, "g")))
		var salt *itemDiffRow
		for i := range items {
			if items[i].ProductName == "Salt" {
				salt = &items[i]
			}
		}
		if salt == nil || !salt.OnlyA || salt.OnlyB {
			t.Fatalf("salt row = %+v", salt)
		}
	})

	t.Run("item only in B", func(t *testing.T) {
		_, items := diffSides(side(4, item("Rice", 200, "g")), side(4, item("Rice", 200, "g"), item("Pepper", 2, "g")))
		var pepper *itemDiffRow
		for i := range items {
			if items[i].ProductName == "Pepper" {
				pepper = &items[i]
			}
		}
		if pepper == nil || !pepper.OnlyB || pepper.OnlyA {
			t.Fatalf("pepper row = %+v", pepper)
		}
	})

	t.Run("duplicate product names within one side are both kept", func(t *testing.T) {
		a := side(4,
			ConflictItem{ProductName: "Oil", Amount: 1, UnitName: "tbsp", ProductUnitID: "oil-1"},
			ConflictItem{ProductName: "Oil", Amount: 2, UnitName: "tbsp", ProductUnitID: "oil-2"},
		)
		b := side(4)
		_, items := diffSides(a, b)
		n := 0
		for _, r := range items {
			if r.ProductName == "Oil" {
				n++
			}
		}
		if n != 2 {
			t.Fatalf("want 2 Oil rows, got %d (%+v)", n, items)
		}
	})

	t.Run("ordering is stable regardless of input order", func(t *testing.T) {
		a := side(4, item("Zucchini", 1, "pc"), item("Apple", 2, "pc"), item("Mango", 1, "pc"))
		b := side(4, item("Mango", 1, "pc"), item("Apple", 2, "pc"), item("Zucchini", 1, "pc"))
		_, items := diffSides(a, b)
		got := make([]string, len(items))
		for i, r := range items {
			got[i] = r.ProductName
		}
		want := []string{"Apple", "Mango", "Zucchini"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("order = %v, want %v", got, want)
			}
		}
	})

	t.Run("nil sides do not panic", func(t *testing.T) {
		portions, items := diffSides(nil, nil)
		if portions.Differs || len(items) != 0 {
			t.Fatalf("portions=%+v items=%+v", portions, items)
		}
	})
}
