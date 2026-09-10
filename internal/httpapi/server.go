// Package httpapi is the HTTP surface for recipe sync. It serves a small JSON API
// (health, a manual trigger, the latest run summary, conflict review/resolution —
// see specs/001-recipe-sync/contracts/http-api.md) and a minimal server-rendered
// web UI under /ui (see specs/002-sync-conflict-ui/contracts/ui.md).
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/syncengine"
)

// Engine is the trigger side the handlers need.
type Engine interface {
	Trigger(trigger string, dryRun bool) (runID string, startedAt time.Time, err error)
	Status() (running bool, runID string, startedAt time.Time)
}

// Conflict is one unresolved conflict rendered for review.
type Conflict struct {
	LinkID            int64         `json:"linkId"`
	Name              string        `json:"name"`
	DetectedAt        *time.Time    `json:"detectedAt,omitempty"`
	A                 *ConflictSide `json:"a"`
	B                 *ConflictSide `json:"b"`
	PendingResolution string        `json:"pendingResolution,omitempty"`
}

// ConflictSide is one account's current version of a conflicted recipe.
type ConflictSide struct {
	RecipeID         string         `json:"recipeId"`
	NumberOfPortions int            `json:"numberOfPortions"`
	Items            []ConflictItem `json:"items"`
}

// ConflictItem is one ingredient line in a conflict view.
type ConflictItem struct {
	ProductName    string  `json:"productName"`
	UnitName       string  `json:"unitName"`
	Amount         float64 `json:"amount"`
	ProductUnitID  string  `json:"productUnitId"`
	BrandProductID string  `json:"brandProductId,omitempty"`
}

// Store is the read/write access the handlers need.
type Store interface {
	Ping(ctx context.Context) error
	LatestRunSummary(ctx context.Context) (summary []byte, found bool, err error)
	ListConflicts(ctx context.Context) ([]Conflict, error)
	RecordResolution(ctx context.Context, linkID int64, winner string) (found bool, err error)
}

// Server holds the router and its dependencies.
type Server struct {
	engine        Engine
	store         Store
	token         string
	defaultDryRun bool
	labelA        string
	labelB        string
	sessions      *sessionStore
	log           *slog.Logger
	mux           *http.ServeMux
}

// New builds the Server and its routes. token == "" disables auth (the JSON API
// serves every route open; the /ui pages serve open too). defaultDryRun is the
// dry-run setting a UI-triggered run inherits when the dry-run box is unchecked,
// and the setting the JSON POST /sync always uses (SYNC_DRY_RUN). labelA/labelB
// name the two accounts in the UI.
func New(engine Engine, store Store, token string, defaultDryRun bool, labelA, labelB string, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		engine:        engine,
		store:         store,
		token:         token,
		defaultDryRun: defaultDryRun,
		labelA:        labelA,
		labelB:        labelB,
		sessions:      newSessionStore(),
		log:           log,
		mux:           http.NewServeMux(),
	}

	// JSON API (specs/001-recipe-sync/contracts/http-api.md).
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.Handle("GET /", s.protected(http.HandlerFunc(s.handleIndex)))
	s.mux.Handle("POST /sync", s.protected(http.HandlerFunc(s.handleSync)))
	s.mux.Handle("GET /sync", s.protected(http.HandlerFunc(s.handleSync)))
	s.mux.Handle("GET /sync/last", s.protected(http.HandlerFunc(s.handleLastRun)))
	s.mux.Handle("GET /conflicts", s.protected(http.HandlerFunc(s.handleConflicts)))
	s.mux.Handle("POST /conflicts/{linkID}/resolve", s.protected(http.HandlerFunc(s.handleResolve)))

	// Web UI (specs/002-sync-conflict-ui/contracts/ui.md).
	s.mux.Handle("GET /ui", s.uiProtected(http.HandlerFunc(s.handleDashboard)))
	s.mux.Handle("GET /ui/{$}", s.uiProtected(http.HandlerFunc(s.handleDashboard)))
	s.mux.Handle("POST /ui/sync", s.uiProtected(http.HandlerFunc(s.handleUISync)))
	s.mux.Handle("GET /ui/conflicts", s.uiProtected(http.HandlerFunc(s.handleUIConflicts)))
	s.mux.Handle("POST /ui/conflicts/{linkID}/resolve", s.uiProtected(http.HandlerFunc(s.handleUIResolve)))
	s.mux.HandleFunc("GET /ui/login", s.handleUILoginForm)
	s.mux.HandleFunc("POST /ui/login", s.handleUILogin)
	s.mux.HandleFunc("POST /ui/logout", s.handleUILogout)
	return s
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// protected wraps a handler with bearer-token auth when a token is configured.
func (s *Server) protected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token != "" && r.Header.Get("Authorization") != "Bearer "+s.token {
			writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// uiProtected gates the /ui pages with a session cookie when a token is
// configured. With no token the UI is open, mirroring the JSON API.
func (s *Server) uiProtected(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.token == "" {
			next.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie(sessionCookieName)
		if err != nil || !s.sessions.valid(c.Value) {
			http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func writeRaw(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// compile-time assertion that syncengine.Engine satisfies the Engine interface.
var _ Engine = (*syncengine.Engine)(nil)
