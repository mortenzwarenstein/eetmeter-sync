package httpapi

import (
	"bytes"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/syncengine"
)

//go:embed templates/*.tmpl
var templateFS embed.FS

var uiFuncs = template.FuncMap{
	"humanTime": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.UTC().Format("2006-01-02 15:04:05 MST")
	},
	"humanTimePtr": func(t *time.Time) string {
		if t == nil || t.IsZero() {
			return "—"
		}
		return t.UTC().Format("2006-01-02 15:04:05 MST")
	},
}

// pages maps each content template to a parsed set that also holds the layout.
var pages = func() map[string]*template.Template {
	m := map[string]*template.Template{}
	for _, name := range []string{"dashboard.tmpl", "conflicts.tmpl", "login.tmpl", "message.tmpl"} {
		m[name] = template.Must(template.New("layout.tmpl").Funcs(uiFuncs).
			ParseFS(templateFS, "templates/layout.tmpl", "templates/"+name))
	}
	return m
}()

// baseView carries the fields the layout needs, embedded by every page view.
type baseView struct {
	Title   string
	Flash   string
	Running bool // drives the <meta refresh> in the layout
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data any) {
	t, ok := pages[page]
	if !ok {
		s.log.Error("unknown ui template", "page", page)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout.tmpl", data); err != nil {
		s.log.Error("ui template render failed", "page", page, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = buf.WriteTo(w)
}

type messageView struct {
	baseView
	Message string
}

// renderMessage shows a plain, navigable message page (used for error cases so
// the UI is never a blank or broken page).
func (s *Server) renderMessage(w http.ResponseWriter, status int, msg string) {
	s.render(w, status, "message.tmpl", messageView{
		baseView: baseView{Title: "eetmeter-sync"},
		Message:  msg,
	})
}

// flashMessage maps a ?msg= code to a short banner. Unknown/empty → "".
func flashMessage(code string) string {
	switch code {
	case "started":
		return "Sync started."
	case "already-running":
		return "A sync is already running — nothing else was started."
	case "resolved":
		return "Resolution recorded. It is applied on the next sync."
	case "stale":
		return "That conflict is no longer open."
	case "bad-credentials":
		return "Incorrect secret."
	default:
		return ""
	}
}

// ---- Dashboard (User Story 1) -------------------------------------------------

type dashboardView struct {
	baseView
	HasRun  bool
	Summary syncengine.Summary
	LabelA  string
	LabelB  string
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	v := dashboardView{
		baseView: baseView{Title: "eetmeter-sync", Flash: flashMessage(r.URL.Query().Get("msg"))},
		LabelA:   s.labelA,
		LabelB:   s.labelB,
	}

	raw, ok, err := s.store.LatestRunSummary(r.Context())
	if err != nil {
		s.log.Error("ui: latest run lookup failed", "err", err)
		s.renderMessage(w, http.StatusInternalServerError, "Could not read the last run.")
		return
	}
	if ok {
		if err := json.Unmarshal(raw, &v.Summary); err != nil {
			s.log.Error("ui: latest run summary unparseable", "err", err)
			s.renderMessage(w, http.StatusInternalServerError, "Could not read the last run.")
			return
		}
		v.HasRun = true
		if lbl := v.Summary.Accounts["a"].Label; lbl != "" {
			v.LabelA = lbl
		}
		if lbl := v.Summary.Accounts["b"].Label; lbl != "" {
			v.LabelB = lbl
		}
	}

	running, _, _ := s.engine.Status()
	v.Running = running || (v.HasRun && v.Summary.Result == "running")
	s.render(w, http.StatusOK, "dashboard.tmpl", v)
}

func (s *Server) handleUISync(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	// The UI toggle can turn a single run into a dry run; a globally dry
	// deployment (SYNC_DRY_RUN) cannot be turned back into a real run here.
	dryRun := r.FormValue("dryRun") != "" || s.defaultDryRun

	if _, _, err := s.engine.Trigger("manual", dryRun); err != nil {
		if errors.Is(err, syncengine.ErrAlreadyRunning) {
			http.Redirect(w, r, "/ui?msg=already-running", http.StatusSeeOther)
			return
		}
		s.log.Error("ui: trigger failed", "err", err)
		s.renderMessage(w, http.StatusInternalServerError, "Could not start the sync.")
		return
	}
	http.Redirect(w, r, "/ui?msg=started", http.StatusSeeOther)
}

// ---- Conflicts (User Story 2) ----------------------------------------------

type conflictRowView struct {
	LinkID     int64
	Name       string
	DetectedAt *time.Time
	Portions   diffPair
	Items      []itemDiffRow
	Pending    string // "a" | "b" | ""
}

type conflictsView struct {
	baseView
	LabelA, LabelB string
	Conflicts      []conflictRowView
}

func (s *Server) handleUIConflicts(w http.ResponseWriter, r *http.Request) {
	rows, err := s.store.ListConflicts(r.Context())
	if err != nil {
		s.log.Error("ui: list conflicts failed", "err", err)
		s.renderMessage(w, http.StatusInternalServerError, "Could not read conflicts.")
		return
	}

	v := conflictsView{
		baseView: baseView{Title: "Conflicts", Flash: flashMessage(r.URL.Query().Get("msg"))},
		LabelA:   s.labelA,
		LabelB:   s.labelB,
	}
	for _, c := range rows {
		portions, items := diffSides(c.A, c.B)
		v.Conflicts = append(v.Conflicts, conflictRowView{
			LinkID:     c.LinkID,
			Name:       c.Name,
			DetectedAt: c.DetectedAt,
			Portions:   portions,
			Items:      items,
			Pending:    c.PendingResolution,
		})
	}
	s.render(w, http.StatusOK, "conflicts.tmpl", v)
}

func (s *Server) handleUIResolve(w http.ResponseWriter, r *http.Request) {
	linkID, err := strconv.ParseInt(r.PathValue("linkID"), 10, 64)
	if err != nil {
		s.renderMessage(w, http.StatusBadRequest, "Bad link id.")
		return
	}
	_ = r.ParseForm()
	winner := r.FormValue("winner")
	if winner != "a" && winner != "b" {
		s.renderMessage(w, http.StatusBadRequest, "Pick account A or B as the winner.")
		return
	}

	found, err := s.store.RecordResolution(r.Context(), linkID, winner)
	if err != nil {
		s.log.Error("ui: record resolution failed", "linkId", linkID, "err", err)
		s.renderMessage(w, http.StatusInternalServerError, "Could not record the resolution.")
		return
	}
	if !found {
		http.Redirect(w, r, "/ui/conflicts?msg=stale", http.StatusSeeOther)
		return
	}

	if r.FormValue("action") == "save_and_sync" {
		if _, _, err := s.engine.Trigger("manual", s.defaultDryRun); err != nil {
			if errors.Is(err, syncengine.ErrAlreadyRunning) {
				http.Redirect(w, r, "/ui/conflicts?msg=already-running", http.StatusSeeOther)
				return
			}
			s.log.Error("ui: trigger after resolve failed", "err", err)
			s.renderMessage(w, http.StatusInternalServerError, "Resolution saved, but the sync could not start.")
			return
		}
	}
	http.Redirect(w, r, "/ui/conflicts?msg=resolved", http.StatusSeeOther)
}

// ---- Sign-in (User Story 4) ----------------------------------------------

type loginView struct {
	baseView
	Error string
}

func (s *Server) handleUILoginForm(w http.ResponseWriter, r *http.Request) {
	if s.token == "" {
		http.Redirect(w, r, "/ui", http.StatusSeeOther)
		return
	}
	var errMsg string
	if r.URL.Query().Get("msg") == "bad-credentials" {
		errMsg = flashMessage("bad-credentials")
	}
	s.render(w, http.StatusOK, "login.tmpl", loginView{
		baseView: baseView{Title: "Sign in"},
		Error:    errMsg,
	})
}

func (s *Server) handleUILogin(w http.ResponseWriter, r *http.Request) {
	if s.token == "" {
		http.Redirect(w, r, "/ui", http.StatusSeeOther)
		return
	}
	_ = r.ParseForm()
	secret := r.FormValue("secret")
	if subtle.ConstantTimeCompare([]byte(secret), []byte(s.token)) != 1 {
		http.Redirect(w, r, "/ui/login?msg=bad-credentials", http.StatusSeeOther)
		return
	}
	setSessionCookie(w, r, s.sessions.create())
	http.Redirect(w, r, "/ui", http.StatusSeeOther)
}

func (s *Server) handleUILogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookieName); err == nil {
		s.sessions.revoke(c.Value)
	}
	clearSessionCookie(w, r)
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
}
