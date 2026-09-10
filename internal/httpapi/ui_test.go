package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/syncengine"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func uiServer(e Engine, s Store, token string, defaultDryRun bool) *Server {
	return New(e, s, token, defaultDryRun, "Alice", "Bob", quiet())
}

// form issues a request with an optional url-encoded body and cookies.
func form(t *testing.T, srv *Server, method, path, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for _, c := range cookies {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func setCookie(w *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == sessionCookieName {
			return c
		}
	}
	return nil
}

// ---- User Story 1: dashboard + trigger --------------------------------------

func TestUI_Dashboard_NoRun(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{summaryOK: false}, "", false)
	w := form(t, srv, "GET", "/ui", "")
	if w.Code != 200 {
		t.Fatalf("got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "No runs yet") {
		t.Fatalf("missing empty state: %s", w.Body)
	}
	if strings.Contains(w.Body.String(), `http-equiv="refresh"`) {
		t.Fatal("meta refresh should be absent when nothing is running")
	}
}

func TestUI_Dashboard_WithSummary(t *testing.T) {
	fs := &fakeStore{
		summaryOK: true,
		summary:   []byte(`{"result":"succeeded","trigger":"manual","runId":"r1","accounts":{"a":{"label":"Alice"},"b":{"label":"Bob"}},"counts":{"createdInB":2,"updatedInA":1}}`),
	}
	srv := uiServer(&fakeEngine{}, fs, "", false)
	w := form(t, srv, "GET", "/ui", "")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "succeeded") {
		t.Fatalf("got %d body %s", w.Code, body)
	}
	if !strings.Contains(body, ">2<") || !strings.Contains(body, ">1<") {
		t.Fatalf("counts not rendered: %s", body)
	}
}

func TestUI_Dashboard_RunningShowsMetaRefresh(t *testing.T) {
	fs := &fakeStore{summaryOK: true, summary: []byte(`{"result":"running","counts":{}}`)}
	srv := uiServer(&fakeEngine{}, fs, "", false)
	w := form(t, srv, "GET", "/ui", "")
	if !strings.Contains(w.Body.String(), `http-equiv="refresh"`) {
		t.Fatalf("expected meta refresh while running: %s", w.Body)
	}
}

func TestUI_Sync_Started(t *testing.T) {
	eng := &fakeEngine{}
	srv := uiServer(eng, &fakeStore{}, "", false)
	w := form(t, srv, "POST", "/ui/sync", "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui?msg=started" {
		t.Fatalf("got %d loc %q", w.Code, w.Header().Get("Location"))
	}
	if !eng.triggered {
		t.Fatal("engine was not triggered")
	}
}

func TestUI_Sync_AlreadyRunning(t *testing.T) {
	eng := &fakeEngine{triggerErr: syncengine.ErrAlreadyRunning}
	srv := uiServer(eng, &fakeStore{}, "", false)
	w := form(t, srv, "POST", "/ui/sync", "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui?msg=already-running" {
		t.Fatalf("got %d loc %q", w.Code, w.Header().Get("Location"))
	}
}

// ---- User Story 3: dry run -------------------------------------------------

func TestUI_Sync_DryRunCheckbox(t *testing.T) {
	eng := &fakeEngine{}
	srv := uiServer(eng, &fakeStore{}, "", false)
	form(t, srv, "POST", "/ui/sync", "dryRun=1")
	if !eng.gotDryRun {
		t.Fatal("checkbox should make the run a dry run")
	}
}

func TestUI_Sync_GlobalDryRunNotOverridable(t *testing.T) {
	eng := &fakeEngine{}
	srv := uiServer(eng, &fakeStore{}, "", true) // SYNC_DRY_RUN equivalent
	form(t, srv, "POST", "/ui/sync", "")         // box unchecked
	if !eng.gotDryRun {
		t.Fatal("a globally dry deployment must stay dry from the UI")
	}
}

func TestUI_Dashboard_DryRunBadge(t *testing.T) {
	fs := &fakeStore{summaryOK: true, summary: []byte(`{"result":"succeeded","dryRun":true,"counts":{}}`)}
	srv := uiServer(&fakeEngine{}, fs, "", false)
	w := form(t, srv, "GET", "/ui", "")
	if !strings.Contains(w.Body.String(), "DRY RUN") {
		t.Fatalf("missing dry-run badge: %s", w.Body)
	}
}

// ---- User Story 2: conflicts --------------------------------------------

func TestUI_Conflicts_List(t *testing.T) {
	fs := &fakeStore{conflicts: []Conflict{{
		LinkID: 42, Name: "Curry",
		A: &ConflictSide{NumberOfPortions: 4, Items: []ConflictItem{{ProductName: "Coconut milk", Amount: 400, UnitName: "ml"}}},
		B: &ConflictSide{NumberOfPortions: 4, Items: []ConflictItem{{ProductName: "Coconut milk", Amount: 200, UnitName: "ml"}}},
	}}}
	srv := uiServer(&fakeEngine{}, fs, "", false)
	w := form(t, srv, "GET", "/ui/conflicts", "")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Curry") {
		t.Fatalf("got %d body %s", w.Code, body)
	}
	if !strings.Contains(body, `class="diff"`) {
		t.Fatalf("differing amount row not marked: %s", body)
	}
	if !strings.Contains(body, "Alice") || !strings.Contains(body, "Bob") {
		t.Fatalf("account labels missing: %s", body)
	}
}

func TestUI_Conflicts_Empty(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{}, "", false)
	w := form(t, srv, "GET", "/ui/conflicts", "")
	if !strings.Contains(w.Body.String(), "No unresolved conflicts") {
		t.Fatalf("missing empty state: %s", w.Body)
	}
}

func TestUI_Resolve_Save(t *testing.T) {
	fs := &fakeStore{resolveFound: true}
	eng := &fakeEngine{}
	srv := uiServer(eng, fs, "", false)
	w := form(t, srv, "POST", "/ui/conflicts/42/resolve", "winner=a&action=save")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/conflicts?msg=resolved" {
		t.Fatalf("got %d loc %q", w.Code, w.Header().Get("Location"))
	}
	if fs.gotWinner != "a" || fs.gotLinkID != 42 {
		t.Fatalf("recorded winner=%q link=%d", fs.gotWinner, fs.gotLinkID)
	}
	if eng.triggered {
		t.Fatal("plain save must not trigger a sync")
	}
}

func TestUI_Resolve_SaveAndSync(t *testing.T) {
	fs := &fakeStore{resolveFound: true}
	eng := &fakeEngine{}
	srv := uiServer(eng, fs, "", false)
	w := form(t, srv, "POST", "/ui/conflicts/7/resolve", "winner=b&action=save_and_sync")
	if w.Header().Get("Location") != "/ui/conflicts?msg=resolved" {
		t.Fatalf("loc %q", w.Header().Get("Location"))
	}
	if !eng.triggered {
		t.Fatal("save_and_sync must trigger a sync")
	}
}

func TestUI_Resolve_SaveAndSync_AlreadyRunning(t *testing.T) {
	fs := &fakeStore{resolveFound: true}
	eng := &fakeEngine{triggerErr: syncengine.ErrAlreadyRunning}
	srv := uiServer(eng, fs, "", false)
	w := form(t, srv, "POST", "/ui/conflicts/7/resolve", "winner=b&action=save_and_sync")
	if w.Header().Get("Location") != "/ui/conflicts?msg=already-running" {
		t.Fatalf("loc %q", w.Header().Get("Location"))
	}
	if fs.gotWinner != "b" {
		t.Fatal("resolution must still be recorded when a run is in progress")
	}
}

func TestUI_Resolve_Stale(t *testing.T) {
	fs := &fakeStore{resolveFound: false}
	srv := uiServer(&fakeEngine{}, fs, "", false)
	w := form(t, srv, "POST", "/ui/conflicts/42/resolve", "winner=a&action=save")
	if w.Header().Get("Location") != "/ui/conflicts?msg=stale" {
		t.Fatalf("loc %q", w.Header().Get("Location"))
	}
}

func TestUI_Resolve_BadWinner(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{}, "", false)
	if w := form(t, srv, "POST", "/ui/conflicts/42/resolve", "winner=x"); w.Code != http.StatusBadRequest {
		t.Fatalf("got %d", w.Code)
	}
	if w := form(t, srv, "POST", "/ui/conflicts/abc/resolve", "winner=a"); w.Code != http.StatusBadRequest {
		t.Fatalf("bad link id got %d", w.Code)
	}
}

// ---- User Story 4: gated access -----------------------------------------

func TestUI_Auth_OpenWhenNoToken(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{}, "", false)
	if w := form(t, srv, "GET", "/ui", ""); w.Code != 200 {
		t.Fatalf("dashboard got %d", w.Code)
	}
	w := form(t, srv, "GET", "/ui/login", "")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui" {
		t.Fatalf("login with no token should redirect to /ui, got %d %q", w.Code, w.Header().Get("Location"))
	}
}

func TestUI_Auth_RedirectWhenNoCookie(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{}, "s3kret", false)
	for _, path := range []string{"/ui", "/ui/conflicts"} {
		w := form(t, srv, "GET", path, "")
		if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui/login" {
			t.Fatalf("%s got %d %q", path, w.Code, w.Header().Get("Location"))
		}
	}
}

func TestUI_Auth_WrongSecret(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{}, "s3kret", false)
	w := form(t, srv, "POST", "/ui/login", "secret=nope")
	if w.Header().Get("Location") != "/ui/login?msg=bad-credentials" {
		t.Fatalf("loc %q", w.Header().Get("Location"))
	}
	if setCookie(w) != nil {
		t.Fatal("no session cookie should be set on a bad login")
	}
}

func TestUI_Auth_RightSecret_ThenAccess(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{}, "s3kret", false)
	w := form(t, srv, "POST", "/ui/login", "secret=s3kret")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/ui" {
		t.Fatalf("got %d loc %q", w.Code, w.Header().Get("Location"))
	}
	c := setCookie(w)
	if c == nil || !c.HttpOnly {
		t.Fatalf("expected an HttpOnly session cookie, got %+v", c)
	}
	if got := form(t, srv, "GET", "/ui", "", c); got.Code != 200 {
		t.Fatalf("authed dashboard got %d", got.Code)
	}
}

func TestUI_Auth_Logout(t *testing.T) {
	srv := uiServer(&fakeEngine{}, &fakeStore{}, "s3kret", false)
	login := form(t, srv, "POST", "/ui/login", "secret=s3kret")
	c := setCookie(login)

	out := form(t, srv, "POST", "/ui/logout", "", c)
	if out.Code != http.StatusSeeOther || out.Header().Get("Location") != "/ui/login" {
		t.Fatalf("logout got %d loc %q", out.Code, out.Header().Get("Location"))
	}
	if cleared := setCookie(out); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("logout should expire the cookie, got %+v", cleared)
	}
	if after := form(t, srv, "GET", "/ui", "", c); after.Code != http.StatusSeeOther {
		t.Fatalf("access after logout should redirect, got %d", after.Code)
	}
}

func TestUI_Auth_NoSecretLeak(t *testing.T) {
	const tok = "super-secret-token-value"
	srv := uiServer(&fakeEngine{}, &fakeStore{}, tok, false)
	login := form(t, srv, "GET", "/ui/login", "")
	if strings.Contains(login.Body.String(), tok) {
		t.Fatal("login page leaked the token")
	}
	c := setCookie(form(t, srv, "POST", "/ui/login", "secret="+tok))
	dash := form(t, srv, "GET", "/ui", "", c)
	if strings.Contains(dash.Body.String(), tok) {
		t.Fatal("dashboard leaked the token")
	}
}
