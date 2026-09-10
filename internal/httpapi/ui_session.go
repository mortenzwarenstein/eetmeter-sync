package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookieName = "eetmeter_ui_session"
	// sessionTTL is a sliding window: a session stays valid while it is used at
	// least this often. Hardcoded — add a knob only if a need appears.
	sessionTTL = 30 * 24 * time.Hour
)

// session is one signed-in browser. It carries no personal data.
type session struct {
	createdAt time.Time
	lastSeen  time.Time
}

// sessionStore holds live sessions in memory. It is lost on restart, which for a
// single operator means one re-sign-in after a redeploy.
type sessionStore struct {
	mu   sync.Mutex
	byID map[string]*session
	now  func() time.Time // overridable in tests
}

func newSessionStore() *sessionStore {
	return &sessionStore{byID: map[string]*session{}, now: time.Now}
}

// create registers a new session and returns its id (the cookie value).
func (s *sessionStore) create() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("httpapi: crypto/rand failed: " + err.Error())
	}
	id := base64.RawURLEncoding.EncodeToString(b)
	now := s.now()
	s.mu.Lock()
	s.byID[id] = &session{createdAt: now, lastSeen: now}
	s.sweepLocked(now)
	s.mu.Unlock()
	return id
}

// valid reports whether id names a live session. On success it slides the
// expiry forward; on expiry it drops the entry.
func (s *sessionStore) valid(id string) bool {
	if id == "" {
		return false
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweepLocked(now)
	sess, ok := s.byID[id]
	if !ok {
		return false
	}
	if now.Sub(sess.lastSeen) > sessionTTL {
		delete(s.byID, id)
		return false
	}
	sess.lastSeen = now
	return true
}

// revoke drops a session (sign-out).
func (s *sessionStore) revoke(id string) {
	s.mu.Lock()
	delete(s.byID, id)
	s.mu.Unlock()
}

// sweepLocked removes expired entries. Caller holds the lock.
func (s *sessionStore) sweepLocked(now time.Time) {
	for id, sess := range s.byID {
		if now.Sub(sess.lastSeen) > sessionTTL {
			delete(s.byID, id)
		}
	}
}

// secureRequest reports whether the request reached us over HTTPS, directly or
// via a TLS-terminating proxy.
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, id string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    id,
		Path:     "/ui",
		MaxAge:   int(sessionTTL / time.Second),
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/ui",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteLaxMode,
	})
}
