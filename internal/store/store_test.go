package store

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// testStore connects to TEST_DATABASE_URL, migrates, and wipes the data tables.
// The whole file is skipped when the variable is unset.
func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set TEST_DATABASE_URL to run store integration tests")
	}
	ctx := context.Background()
	s, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := s.pool.Exec(ctx,
		`TRUNCATE conflict_resolution, recipe_link, sync_run, account RESTART IDENTITY CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return s
}

func TestMigrate_IsIdempotent(t *testing.T) {
	s := testStore(t)
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
}

func TestUpsertLink_RoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Millisecond)

	in := Link{
		Name: "Bami", ARecipeID: "a1", BRecipeID: "b1",
		ABaselineHash: []byte("0123456789abcdef0123456789abcdef"),
		BBaselineHash: []byte("0123456789abcdef0123456789abcdef"),
		Status:        StatusActive, LastSyncedAt: &now,
	}
	saved, err := s.UpsertLink(ctx, in)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if saved.ID == 0 {
		t.Fatal("no id assigned")
	}

	got, err := s.GetLink(ctx, "Bami")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ARecipeID != "a1" || got.BRecipeID != "b1" || got.Status != StatusActive {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if string(got.ABaselineHash) != string(in.ABaselineHash) {
		t.Fatalf("baseline hash not preserved")
	}

	// upsert again with a changed status; id stays, row updates.
	in.Status = StatusConflict
	in.ConflictDetectedAt = &now
	in.AConflictSnapshot = []byte(`{"Name":"Bami"}`)
	if _, err := s.UpsertLink(ctx, in); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	got, _ = s.GetLink(ctx, "Bami")
	if got.ID != saved.ID || got.Status != StatusConflict || got.ConflictDetectedAt == nil {
		t.Fatalf("update mismatch: %+v", got)
	}
}

func TestResolutionFlow(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	l, _ := s.UpsertLink(ctx, Link{Name: "Curry", ARecipeID: "a1", BRecipeID: "b1", Status: StatusActive})

	// resolution rejected while the link is not in conflict
	if err := s.UpsertResolution(ctx, l.ID, "a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound for active link, got %v", err)
	}

	l.Status = StatusConflict
	_, _ = s.UpsertLink(ctx, l)
	if err := s.UpsertResolution(ctx, l.ID, "a"); err != nil {
		t.Fatalf("upsert resolution: %v", err)
	}
	res, _ := s.ListResolutions(ctx)
	if res[l.ID] != "a" {
		t.Fatalf("resolutions = %v", res)
	}

	// apply clears status, conflict fields, and the resolution row
	now := time.Now().UTC()
	l.LastSyncedAt = &now
	if err := s.ApplyResolution(ctx, l); err != nil {
		t.Fatalf("apply: %v", err)
	}
	got, _ := s.GetLink(ctx, "Curry")
	if got.Status != StatusActive || got.ConflictDetectedAt != nil {
		t.Fatalf("link not reset after resolution: %+v", got)
	}
	res, _ = s.ListResolutions(ctx)
	if len(res) != 0 {
		t.Fatalf("resolution row not deleted: %v", res)
	}
}

func TestListConflicts(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	_, _ = s.UpsertLink(ctx, Link{Name: "OK", ARecipeID: "a1", BRecipeID: "b1", Status: StatusActive})
	l, _ := s.UpsertLink(ctx, Link{
		Name: "Curry", ARecipeID: "a2", BRecipeID: "b2", Status: StatusConflict,
		ConflictDetectedAt: &now,
		AConflictSnapshot:  []byte(`{"Name":"Curry","NumberOfPortions":4,"Items":[]}`),
		BConflictSnapshot:  []byte(`{"Name":"Curry","NumberOfPortions":6,"Items":[]}`),
	})
	_ = s.UpsertResolution(ctx, l.ID, "b")

	rows, err := s.ListConflicts(ctx)
	if err != nil {
		t.Fatalf("list conflicts: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 conflict, got %d", len(rows))
	}
	if rows[0].Name != "Curry" || rows[0].Pending != "b" || len(rows[0].ASnapshot) == 0 {
		t.Fatalf("conflict row = %+v", rows[0])
	}
}

func TestRunLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	if _, err := s.LatestRun(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound with no runs, got %v", err)
	}

	start := time.Now().UTC()
	if err := s.StartRun(ctx, "run-1", "manual", start); err != nil {
		t.Fatalf("start: %v", err)
	}
	// a second running row is rejected by the partial unique index
	if err := s.StartRun(ctx, "run-2", "scheduled", start); err == nil {
		t.Fatal("expected the second concurrent run to be rejected")
	}
	if err := s.SaveRunSummary(ctx, "run-1", []byte(`{"result":"running"}`)); err != nil {
		t.Fatalf("save summary: %v", err)
	}
	if err := s.FinishRun(ctx, "run-1", "succeeded", "", []byte(`{"result":"succeeded"}`), time.Now().UTC()); err != nil {
		t.Fatalf("finish: %v", err)
	}
	got, err := s.LatestRun(ctx)
	if err != nil || got.Status != "succeeded" || got.FinishedAt == nil {
		t.Fatalf("latest run = %+v err %v", got, err)
	}

	// after finishing, a new run can start; stale-running cleanup is a no-op here
	if err := s.ClearStaleRunningRuns(ctx); err != nil {
		t.Fatalf("clear stale: %v", err)
	}
}
