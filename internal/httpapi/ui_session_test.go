package httpapi

import (
	"testing"
	"time"
)

func TestSessionStore(t *testing.T) {
	t.Run("valid right after create", func(t *testing.T) {
		s := newSessionStore()
		id := s.create()
		if !s.valid(id) {
			t.Fatal("new session should be valid")
		}
		if s.valid("nope") || s.valid("") {
			t.Fatal("unknown / empty id should be invalid")
		}
	})

	t.Run("expires past the TTL", func(t *testing.T) {
		now := time.Now()
		s := newSessionStore()
		s.now = func() time.Time { return now }
		id := s.create()

		now = now.Add(sessionTTL + time.Minute)
		if s.valid(id) {
			t.Fatal("session past TTL should be invalid")
		}
		if _, ok := s.byID[id]; ok {
			t.Fatal("expired session should be dropped")
		}
	})

	t.Run("sliding renewal keeps it alive", func(t *testing.T) {
		now := time.Now()
		s := newSessionStore()
		s.now = func() time.Time { return now }
		id := s.create()

		// Use it every 20 days for 100 days; never idle longer than the TTL.
		for i := 0; i < 5; i++ {
			now = now.Add(20 * 24 * time.Hour)
			if !s.valid(id) {
				t.Fatalf("renewal %d: session should still be valid", i)
			}
		}
	})

	t.Run("revoke invalidates", func(t *testing.T) {
		s := newSessionStore()
		id := s.create()
		s.revoke(id)
		if s.valid(id) {
			t.Fatal("revoked session should be invalid")
		}
	})

	t.Run("create sweeps other expired entries", func(t *testing.T) {
		now := time.Now()
		s := newSessionStore()
		s.now = func() time.Time { return now }
		stale := s.create()

		now = now.Add(sessionTTL + time.Hour)
		fresh := s.create()

		if _, ok := s.byID[stale]; ok {
			t.Fatal("stale session should have been swept on create")
		}
		if !s.valid(fresh) {
			t.Fatal("fresh session should be valid")
		}
	})
}
