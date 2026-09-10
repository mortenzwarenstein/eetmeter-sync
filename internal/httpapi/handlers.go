package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/syncengine"
)

func decodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(v)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Ping(r.Context()); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"status": "degraded", "error": "database unreachable",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "eetmeter-sync",
		"routes": []string{
			"GET  /healthz",
			"POST /sync            (also GET /sync for the browser)",
			"GET  /sync/last",
			"GET  /conflicts",
			"POST /conflicts/{linkID}/resolve   body {\"winner\":\"a|b\"} or ?winner=a",
			"GET  /ui              (browser: trigger a sync, resolve conflicts)",
		},
	})
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	runID, startedAt, err := s.engine.Trigger("manual", s.defaultDryRun)
	if errors.Is(err, syncengine.ErrAlreadyRunning) {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":     "sync already running",
			"runId":     runID,
			"startedAt": startedAt.UTC().Format(time.RFC3339),
		})
		return
	}
	if err != nil {
		s.log.Error("trigger failed", "err", err)
		writeError(w, http.StatusInternalServerError, "could not start sync")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"runId":     runID,
		"startedAt": startedAt.UTC().Format(time.RFC3339),
		"trigger":   "manual",
	})
}

func (s *Server) handleLastRun(w http.ResponseWriter, r *http.Request) {
	summary, found, err := s.store.LatestRunSummary(r.Context())
	if err != nil {
		s.log.Error("latest run lookup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read last run")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no run yet")
		return
	}
	writeRaw(w, http.StatusOK, summary)
}

func (s *Server) handleConflicts(w http.ResponseWriter, r *http.Request) {
	conflicts, err := s.store.ListConflicts(r.Context())
	if err != nil {
		s.log.Error("list conflicts failed", "err", err)
		writeError(w, http.StatusInternalServerError, "could not read conflicts")
		return
	}
	if conflicts == nil {
		conflicts = []Conflict{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"conflicts": conflicts})
}

func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	linkID, err := strconv.ParseInt(r.PathValue("linkID"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "linkID must be an integer")
		return
	}

	winner := r.URL.Query().Get("winner")
	if winner == "" {
		var body struct {
			Winner string `json:"winner"`
		}
		_ = decodeJSON(r, &body)
		winner = body.Winner
	}
	if winner != "a" && winner != "b" {
		writeError(w, http.StatusBadRequest, "winner must be 'a' or 'b'")
		return
	}

	found, err := s.store.RecordResolution(r.Context(), linkID, winner)
	if err != nil {
		s.log.Error("record resolution failed", "linkId", linkID, "err", err)
		writeError(w, http.StatusInternalServerError, "could not record resolution")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "no conflict for link "+strconv.FormatInt(linkID, 10))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"linkId":           linkID,
		"winner":           winner,
		"appliesOnNextRun": true,
	})
}
