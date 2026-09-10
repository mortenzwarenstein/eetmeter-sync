package syncengine

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/store"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/uid"
)

// --- in-memory fake Eetmeter account ---

type fakeAccount struct {
	recipes  map[string]*eetmeter.Recipe // id -> recipe
	loginErr error
}

func newFakeAccount(seed ...eetmeter.Recipe) *fakeAccount {
	a := &fakeAccount{recipes: map[string]*eetmeter.Recipe{}}
	for i := range seed {
		r := seed[i]
		r.ID = uid.New()
		a.recipes[r.ID] = &r
	}
	return a
}

type fakeClient struct{ acct *fakeAccount }

func (c *fakeClient) Login(context.Context) error { return c.acct.loginErr }

func (c *fakeClient) ListRecipes(context.Context) ([]eetmeter.Recipe, error) {
	out := make([]eetmeter.Recipe, 0, len(c.acct.recipes))
	for _, r := range c.acct.recipes {
		out = append(out, *r)
	}
	return out, nil
}

// deepCopy detaches Items from any shared backing array — a real API call
// serialises the body, so nothing is aliased across accounts.
func deepCopy(r eetmeter.Recipe) eetmeter.Recipe {
	items := make([]eetmeter.Ingredient, len(r.Items))
	copy(items, r.Items)
	r.Items = items
	return r
}

func (c *fakeClient) CreateRecipe(_ context.Context, r eetmeter.Recipe) (eetmeter.Recipe, error) {
	r = deepCopy(r)
	r.ID = uid.New()
	for i := range r.Items {
		r.Items[i].ID = uid.New()
	}
	c.acct.recipes[r.ID] = &r
	return deepCopy(r), nil
}

func (c *fakeClient) UpdateRecipe(_ context.Context, id string, r eetmeter.Recipe) (eetmeter.Recipe, error) {
	if _, ok := c.acct.recipes[id]; !ok {
		return eetmeter.Recipe{}, errors.New("fake: no such recipe " + id)
	}
	r = deepCopy(r)
	r.ID = id
	c.acct.recipes[id] = &r
	return deepCopy(r), nil
}

// --- harness ---

func newTestEngine(t *testing.T, a, b *fakeAccount) (*Engine, *store.Store) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run engine integration tests")
	}
	ctx := context.Background()
	st, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatalf("store open: %v", err)
	}
	t.Cleanup(st.Close)
	if err := st.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("truncate pool: %v", err)
	}
	_, err = pool.Exec(ctx, `TRUNCATE conflict_resolution, recipe_link, sync_run, account RESTART IDENTITY CASCADE`)
	pool.Close()
	if err != nil {
		t.Fatalf("truncate: %v", err)
	}

	factory := func(ac AccountConfig) Client {
		if ac.Label == "A" {
			return &fakeClient{acct: a}
		}
		return &fakeClient{acct: b}
	}
	eng := New(st,
		AccountConfig{Label: "A", Email: "a@x", Password: "pa"},
		AccountConfig{Label: "B", Email: "b@x", Password: "pb"},
		factory, slog.New(slog.NewTextHandler(io.Discard, nil)), time.Minute)
	return eng, st
}

func recWith(name string, portions int, amount float64) eetmeter.Recipe {
	return eetmeter.Recipe{
		Name: name, NumberOfPortions: portions,
		Items: []eetmeter.Ingredient{{ProductUnitID: "u1", ProductName: "Rice", UnitName: "g", Amount: amount}},
	}
}

func run(t *testing.T, eng *Engine) *Summary {
	t.Helper()
	sum, err := eng.RunSync(context.Background(), "manual", false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return sum
}

func runDry(t *testing.T, eng *Engine) *Summary {
	t.Helper()
	sum, err := eng.RunSync(context.Background(), "manual", true)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	return sum
}

func countByName(a *fakeAccount, name string) int {
	n := 0
	for _, r := range a.recipes {
		if r.Name == name {
			n++
		}
	}
	return n
}

// --- US1: new recipes propagate both ways ---

func TestEngine_US1_CreatesBothDirections(t *testing.T) {
	a := newFakeAccount(recWith("Bami", 4, 300))
	b := newFakeAccount(recWith("Soep", 2, 500))
	eng, _ := newTestEngine(t, a, b)

	sum := run(t, eng)
	if sum.Counts.CreatedInB != 1 || sum.Counts.CreatedInA != 1 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
	if countByName(b, "Bami") != 1 || countByName(a, "Soep") != 1 {
		t.Fatalf("recipes did not propagate: a=%d/Soep b=%d/Bami", countByName(a, "Soep"), countByName(b, "Bami"))
	}

	// second run: nothing changes, no duplicates
	sum2 := run(t, eng)
	if sum2.Counts.CreatedInA != 0 || sum2.Counts.CreatedInB != 0 {
		t.Fatalf("second run created: %+v", sum2.Counts)
	}
	if countByName(a, "Bami") != 1 || countByName(b, "Bami") != 1 {
		t.Fatal("duplicate created on second run")
	}
}

// --- US2: edits propagate ---

func TestEngine_US2_UpdatePropagates(t *testing.T) {
	a := newFakeAccount(recWith("Bami", 4, 300))
	b := newFakeAccount()
	eng, _ := newTestEngine(t, a, b)
	run(t, eng) // establish the link

	// edit in A
	for _, r := range a.recipes {
		if r.Name == "Bami" {
			r.NumberOfPortions = 6
		}
	}
	sum := run(t, eng)
	if sum.Counts.UpdatedInB != 1 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
	var bPortions int
	for _, r := range b.recipes {
		if r.Name == "Bami" {
			bPortions = r.NumberOfPortions
		}
	}
	if bPortions != 6 {
		t.Fatalf("B not updated, portions = %d", bPortions)
	}

	// edit in B this time
	for _, r := range b.recipes {
		if r.Name == "Bami" {
			r.Items[0].Amount = 350
		}
	}
	sum = run(t, eng)
	if sum.Counts.UpdatedInA != 1 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
}

// --- US3: conflicts ---

func TestEngine_US3_ConflictFlaggedThenResolved(t *testing.T) {
	a := newFakeAccount(recWith("Curry", 4, 400))
	b := newFakeAccount()
	eng, st := newTestEngine(t, a, b)
	run(t, eng) // link established, both = (4, 400)

	// diverge both sides
	for _, r := range a.recipes {
		if r.Name == "Curry" {
			r.NumberOfPortions = 8
		}
	}
	for _, r := range b.recipes {
		if r.Name == "Curry" {
			r.Items[0].Amount = 500
		}
	}
	sum := run(t, eng)
	if sum.Counts.FlaggedConflicts != 1 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
	// neither side written
	for _, r := range a.recipes {
		if r.Name == "Curry" && r.NumberOfPortions != 8 {
			t.Fatal("A was modified during conflict")
		}
	}

	conflicts, err := st.ListConflicts(context.Background())
	if err != nil || len(conflicts) != 1 {
		t.Fatalf("conflicts: %v %d", err, len(conflicts))
	}

	// resolve in favour of A, then run
	if err := st.UpsertResolution(context.Background(), conflicts[0].LinkID, "a"); err != nil {
		t.Fatalf("record resolution: %v", err)
	}
	sum = run(t, eng)
	if sum.Counts.ResolutionsApplied != 1 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
	var bPortions int
	for _, r := range b.recipes {
		if r.Name == "Curry" {
			bPortions = r.NumberOfPortions
		}
	}
	if bPortions != 8 {
		t.Fatalf("resolution not applied to B, portions = %d", bPortions)
	}
	after, _ := st.ListConflicts(context.Background())
	if len(after) != 0 {
		t.Fatalf("conflict still listed after resolution: %d", len(after))
	}
}

// --- edges ---

func TestEngine_DeletionRetiresLink(t *testing.T) {
	a := newFakeAccount(recWith("Bami", 4, 300))
	b := newFakeAccount()
	eng, st := newTestEngine(t, a, b)
	run(t, eng)

	// delete on A
	for id, r := range a.recipes {
		if r.Name == "Bami" {
			delete(a.recipes, id)
		}
	}
	sum := run(t, eng)
	if sum.Counts.LinksRetired != 1 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
	if countByName(b, "Bami") != 1 {
		t.Fatal("surviving copy in B was touched")
	}
	l, _ := st.GetLink(context.Background(), "Bami")
	if l.Status != store.StatusRetired {
		t.Fatalf("link status = %q", l.Status)
	}

	// re-create with same name on A: retired link blocks re-link, reports skipped
	a.recipes[uid.New()] = ptr(recWith("Bami", 4, 300))
	sum = run(t, eng)
	if sum.Counts.Skipped != 1 || sum.Counts.CreatedInB != 0 {
		t.Fatalf("reappearance not skipped: %+v", sum.Counts)
	}
}

func TestEngine_DryRun_PreviewsWithoutWriting(t *testing.T) {
	a := newFakeAccount(recWith("Bami", 4, 300), recWith("Soep", 2, 500))
	b := newFakeAccount(recWith("Curry", 3, 200))
	eng, st := newTestEngine(t, a, b)

	sum := runDry(t, eng)
	if !sum.DryRun {
		t.Fatal("summary should be marked dryRun")
	}
	if sum.Counts.CreatedInB != 2 || sum.Counts.CreatedInA != 1 {
		t.Fatalf("preview counts = %+v", sum.Counts)
	}
	// nothing actually written
	if len(a.recipes) != 2 || len(b.recipes) != 1 {
		t.Fatalf("dry run mutated accounts: a=%d b=%d", len(a.recipes), len(b.recipes))
	}
	if links, _ := st.ListLinks(context.Background()); len(links) != 0 {
		t.Fatalf("dry run wrote %d links", len(links))
	}

	// a real run afterwards still does the work
	real := run(t, eng)
	if real.Counts.CreatedInB != 2 || real.Counts.CreatedInA != 1 {
		t.Fatalf("real run after dry run = %+v", real.Counts)
	}
	if len(b.recipes) != 3 {
		t.Fatalf("real run did not create: b=%d", len(b.recipes))
	}
}

func TestEngine_DuplicateNameSkipped(t *testing.T) {
	a := newFakeAccount(recWith("Dubbel", 1, 10), recWith("Dubbel", 2, 20))
	b := newFakeAccount()
	eng, _ := newTestEngine(t, a, b)
	sum := run(t, eng)
	if sum.Counts.Skipped != 1 || sum.Counts.CreatedInB != 0 {
		t.Fatalf("counts = %+v", sum.Counts)
	}
}

func TestEngine_AuthFailureAbortsWithNoWrites(t *testing.T) {
	a := newFakeAccount(recWith("Bami", 4, 300))
	b := newFakeAccount()
	b.loginErr = eetmeter.ErrAuth
	eng, st := newTestEngine(t, a, b)

	sum, err := eng.RunSync(context.Background(), "manual", false)
	if err == nil {
		t.Fatal("expected a run error")
	}
	if sum.Result != "failed" {
		t.Fatalf("summary result = %q", sum.Result)
	}
	if len(b.recipes) != 0 {
		t.Fatal("B was written despite auth failure")
	}
	last, _ := st.LatestRun(context.Background())
	if last.Status != "failed" || last.Error == "" {
		t.Fatalf("run row = %+v", last)
	}
}

func TestEngine_AlreadyRunning(t *testing.T) {
	a := newFakeAccount()
	b := newFakeAccount()
	eng, _ := newTestEngine(t, a, b)

	if _, _, err := eng.begin(); err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer eng.end()
	if _, _, err := eng.Trigger("manual", false); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("want ErrAlreadyRunning, got %v", err)
	}
}

func ptr(r eetmeter.Recipe) *eetmeter.Recipe {
	r.ID = uid.New()
	return &r
}
