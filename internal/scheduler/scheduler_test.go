package scheduler

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"
)

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNext_LaterToday(t *testing.T) {
	loc := time.UTC
	s := New(6, 0, loc, nil, quietLogger())
	now := time.Date(2026, 3, 1, 5, 0, 0, 0, loc)
	got := s.Next(now)
	want := time.Date(2026, 3, 1, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("Next = %s, want %s", got, want)
	}
}

func TestNext_RollsToTomorrowWhenPast(t *testing.T) {
	loc := time.UTC
	s := New(6, 0, loc, nil, quietLogger())
	now := time.Date(2026, 3, 1, 6, 0, 0, 0, loc) // exactly the time -> must be strictly after
	got := s.Next(now)
	want := time.Date(2026, 3, 2, 6, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("Next = %s, want %s", got, want)
	}
}

func TestNext_DSTSpringForwardGapNormalizesForward(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Skip("no tzdata")
	}
	// 2026-03-29: clocks jump 02:00 -> 03:00 in NL. 02:30 does not exist.
	s := New(2, 30, loc, nil, quietLogger())
	now := time.Date(2026, 3, 29, 1, 0, 0, 0, loc)
	got := s.Next(now)
	if got.Hour() == 2 {
		t.Fatalf("Next returned a non-existent 02:%02d local time: %s", got.Minute(), got)
	}
	if got.Before(now) {
		t.Fatalf("Next %s is before now %s", got, now)
	}
}

func TestRun_FiresThenStopsOnContextCancel(t *testing.T) {
	loc := time.UTC
	fired := make(chan string, 1)
	s := New(0, 0, loc, func(reason string) error {
		select {
		case fired <- reason:
		default:
		}
		return nil
	}, quietLogger())
	// Pretend "now" is just before midnight so the timer is tiny.
	s.now = func() time.Time { return time.Date(2026, 1, 1, 23, 59, 59, 900*int(time.Millisecond), loc) }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()

	select {
	case reason := <-fired:
		if reason != "scheduled" {
			t.Fatalf("fired with reason %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scheduler did not fire")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}
