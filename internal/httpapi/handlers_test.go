package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/syncengine"
)

type fakeEngine struct {
	running    bool
	runID      string
	startedAt  time.Time
	triggerErr error
	triggered  bool
	gotDryRun  bool
}

func (f *fakeEngine) Trigger(_ string, dryRun bool) (string, time.Time, error) {
	f.gotDryRun = dryRun
	f.triggered = true
	if f.triggerErr != nil {
		return f.runID, f.startedAt, f.triggerErr
	}
	f.runID = "run-1"
	f.startedAt = time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC)
	return f.runID, f.startedAt, nil
}
func (f *fakeEngine) Status() (bool, string, time.Time) { return f.running, f.runID, f.startedAt }

type fakeStore struct {
	pingErr      error
	summary      []byte
	summaryOK    bool
	conflicts    []Conflict
	resolveFound bool
	resolveErr   error
	gotWinner    string
	gotLinkID    int64
}

func (f *fakeStore) Ping(context.Context) error { return f.pingErr }
func (f *fakeStore) LatestRunSummary(context.Context) ([]byte, bool, error) {
	return f.summary, f.summaryOK, nil
}
func (f *fakeStore) ListConflicts(context.Context) ([]Conflict, error) { return f.conflicts, nil }
func (f *fakeStore) RecordResolution(_ context.Context, linkID int64, winner string) (bool, error) {
	f.gotLinkID = linkID
	f.gotWinner = winner
	return f.resolveFound, f.resolveErr
}

func newTestServer(e Engine, s Store) *Server {
	return New(e, s, "", false, "a", "b", slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func do(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func TestHealth(t *testing.T) {
	ok := newTestServer(&fakeEngine{}, &fakeStore{})
	if w := do(t, ok, "GET", "/healthz", ""); w.Code != 200 {
		t.Fatalf("healthy: got %d", w.Code)
	}
	bad := newTestServer(&fakeEngine{}, &fakeStore{pingErr: errors.New("down")})
	if w := do(t, bad, "GET", "/healthz", ""); w.Code != 503 {
		t.Fatalf("degraded: got %d", w.Code)
	}
}

func TestSync_202(t *testing.T) {
	srv := newTestServer(&fakeEngine{}, &fakeStore{})
	w := do(t, srv, "POST", "/sync", "")
	if w.Code != http.StatusAccepted {
		t.Fatalf("got %d body %s", w.Code, w.Body)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["runId"] != "run-1" || resp["trigger"] != "manual" {
		t.Fatalf("resp = %v", resp)
	}
}

func TestSync_409WhenRunning(t *testing.T) {
	eng := &fakeEngine{
		runID:      "in-flight",
		startedAt:  time.Date(2026, 9, 10, 6, 0, 0, 0, time.UTC),
		triggerErr: syncengine.ErrAlreadyRunning,
	}
	srv := newTestServer(eng, &fakeStore{})
	w := do(t, srv, "POST", "/sync", "")
	if w.Code != http.StatusConflict {
		t.Fatalf("got %d", w.Code)
	}
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "sync already running" || resp["runId"] != "in-flight" {
		t.Fatalf("resp = %v", resp)
	}
}

func TestSync_GETAlsoTriggers(t *testing.T) {
	srv := newTestServer(&fakeEngine{}, &fakeStore{})
	if w := do(t, srv, "GET", "/sync", ""); w.Code != http.StatusAccepted {
		t.Fatalf("GET /sync got %d", w.Code)
	}
}

func TestLastRun(t *testing.T) {
	none := newTestServer(&fakeEngine{}, &fakeStore{summaryOK: false})
	if w := do(t, none, "GET", "/sync/last", ""); w.Code != 404 {
		t.Fatalf("no run: got %d", w.Code)
	}
	has := newTestServer(&fakeEngine{}, &fakeStore{summaryOK: true, summary: []byte(`{"result":"succeeded","counts":{"createdInB":2}}`)})
	w := do(t, has, "GET", "/sync/last", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"createdInB":2`) {
		t.Fatalf("got %d body %s", w.Code, w.Body)
	}
}

func TestConflicts_ListShape(t *testing.T) {
	fs := &fakeStore{conflicts: []Conflict{{
		LinkID: 42, Name: "Curry",
		A: &ConflictSide{RecipeID: "a1", NumberOfPortions: 4, Items: []ConflictItem{}},
		B: &ConflictSide{RecipeID: "b1", NumberOfPortions: 4, Items: []ConflictItem{}},
	}}}
	srv := newTestServer(&fakeEngine{}, fs)
	w := do(t, srv, "GET", "/conflicts", "")
	if w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
	var resp struct {
		Conflicts []Conflict `json:"conflicts"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Conflicts) != 1 || resp.Conflicts[0].Name != "Curry" || resp.Conflicts[0].LinkID != 42 {
		t.Fatalf("conflicts = %+v", resp.Conflicts)
	}
}

func TestConflicts_EmptyIsArrayNotNull(t *testing.T) {
	srv := newTestServer(&fakeEngine{}, &fakeStore{})
	w := do(t, srv, "GET", "/conflicts", "")
	if !strings.Contains(w.Body.String(), `"conflicts":[]`) {
		t.Fatalf("body = %s", w.Body)
	}
}

func TestResolve(t *testing.T) {
	t.Run("happy path via body", func(t *testing.T) {
		fs := &fakeStore{resolveFound: true}
		srv := newTestServer(&fakeEngine{}, fs)
		w := do(t, srv, "POST", "/conflicts/42/resolve", `{"winner":"a"}`)
		if w.Code != 200 || fs.gotWinner != "a" {
			t.Fatalf("got %d winner %q", w.Code, fs.gotWinner)
		}
	})
	t.Run("happy path via query", func(t *testing.T) {
		fs := &fakeStore{resolveFound: true}
		srv := newTestServer(&fakeEngine{}, fs)
		w := do(t, srv, "POST", "/conflicts/42/resolve?winner=b", "")
		if w.Code != 200 || fs.gotWinner != "b" {
			t.Fatalf("got %d winner %q", w.Code, fs.gotWinner)
		}
	})
	t.Run("bad winner -> 400", func(t *testing.T) {
		srv := newTestServer(&fakeEngine{}, &fakeStore{})
		if w := do(t, srv, "POST", "/conflicts/42/resolve", `{"winner":"c"}`); w.Code != 400 {
			t.Fatalf("got %d", w.Code)
		}
	})
	t.Run("not in conflict -> 404", func(t *testing.T) {
		srv := newTestServer(&fakeEngine{}, &fakeStore{resolveFound: false})
		if w := do(t, srv, "POST", "/conflicts/42/resolve?winner=a", ""); w.Code != 404 {
			t.Fatalf("got %d", w.Code)
		}
	})
	t.Run("bad link id -> 400", func(t *testing.T) {
		srv := newTestServer(&fakeEngine{}, &fakeStore{})
		if w := do(t, srv, "POST", "/conflicts/abc/resolve?winner=a", ""); w.Code != 400 {
			t.Fatalf("got %d", w.Code)
		}
	})
}

func TestBearerAuth(t *testing.T) {
	srv := New(&fakeEngine{}, &fakeStore{}, "sekret", false, "a", "b", slog.New(slog.NewTextHandler(io.Discard, nil)))

	if w := do(t, srv, "GET", "/healthz", ""); w.Code != 200 {
		t.Fatalf("healthz should be exempt, got %d", w.Code)
	}
	if w := do(t, srv, "POST", "/sync", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token: got %d", w.Code)
	}
	r := httptest.NewRequest("POST", "/sync", nil)
	r.Header.Set("Authorization", "Bearer sekret")
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	if w.Code != http.StatusAccepted {
		t.Fatalf("with token: got %d", w.Code)
	}
}
