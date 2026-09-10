package fingerprint

import (
	"testing"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
)

func recipe(portions int, items ...eetmeter.Ingredient) eetmeter.Recipe {
	return eetmeter.Recipe{Name: "X", NumberOfPortions: portions, Items: items}
}

func ing(unitID, name string, amount float64) eetmeter.Ingredient {
	return eetmeter.Ingredient{ProductUnitID: unitID, ProductName: name, UnitName: "gram", Amount: amount}
}

func TestOf_IgnoresIngredientOrder(t *testing.T) {
	a := recipe(2, ing("u1", "Rice", 100), ing("u2", "Beans", 50))
	b := recipe(2, ing("u2", "Beans", 50), ing("u1", "Rice", 100))
	if Of(a) != Of(b) {
		t.Fatal("reordering ingredients changed the fingerprint")
	}
}

func TestOf_IgnoresIDsAndName(t *testing.T) {
	a := recipe(2, ing("u1", "Rice", 100))
	a.ID = "recipe-a"
	a.Name = "Name A"
	a.Items[0].ID = "line-a"

	b := recipe(2, ing("u1", "Rice", 100))
	b.ID = "recipe-b"
	b.Name = "Totally Different Name"
	b.Items[0].ID = "line-b"

	if Of(a) != Of(b) {
		t.Fatal("server-assigned IDs or the name leaked into the fingerprint")
	}
}

func TestOf_AbsorbsFloatNoise(t *testing.T) {
	a := recipe(1, ing("u1", "Rice", 300))
	b := recipe(1, ing("u1", "Rice", 300.0000001))
	if Of(a) != Of(b) {
		t.Fatal("sub-milli float noise registered as a change")
	}
}

func TestOf_DetectsRealChanges(t *testing.T) {
	base := recipe(2, ing("u1", "Rice", 100))
	cases := map[string]eetmeter.Recipe{
		"portion count": recipe(3, ing("u1", "Rice", 100)),
		"amount":        recipe(2, ing("u1", "Rice", 120)),
		"extra ingredient": recipe(2,
			ing("u1", "Rice", 100), ing("u9", "Salt", 2)),
		"product ref": recipe(2, ing("u2", "Rice", 100)),
	}
	for name, changed := range cases {
		t.Run(name, func(t *testing.T) {
			if Of(base) == Of(changed) {
				t.Fatalf("%s change did not move the fingerprint", name)
			}
		})
	}
}

func TestEqual_AgainstStoredBytes(t *testing.T) {
	r := recipe(1, ing("u1", "Rice", 100))
	sum := Of(r)
	if !Equal(sum.Bytes(), sum) {
		t.Fatal("Equal returned false for a matching stored hash")
	}
	if Equal(nil, sum) {
		t.Fatal("Equal returned true for a nil stored hash")
	}
	if Equal([]byte{1, 2, 3}, sum) {
		t.Fatal("Equal returned true for a short/garbage stored hash")
	}
}
