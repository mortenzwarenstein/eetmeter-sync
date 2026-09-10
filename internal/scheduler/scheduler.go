// Package scheduler fires a daily sync at a configured wall-clock time.
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// Trigger is the callback the scheduler invokes. It should return quickly (the
// engine's Trigger starts the run in the background); an "already running" error
// is expected occasionally and is not fatal.
type Trigger func(reason string) error

// Scheduler runs Trigger once per day at HH:MM in a fixed location.
type Scheduler struct {
	hour, min int
	loc       *time.Location
	fire      Trigger
	log       *slog.Logger
	now       func() time.Time // overridable in tests
}

// New builds a Scheduler. loc must not be nil.
func New(hour, min int, loc *time.Location, fire Trigger, log *slog.Logger) *Scheduler {
	if log == nil {
		log = slog.Default()
	}
	return &Scheduler{hour: hour, min: min, loc: loc, fire: fire, log: log, now: time.Now}
}

// Next returns the next occurrence of the configured time strictly after t.
// If HH:MM does not exist on the target day (spring-forward DST gap),
// time.Date normalizes it forward to the next valid instant.
func (s *Scheduler) Next(t time.Time) time.Time {
	t = t.In(s.loc)
	cand := time.Date(t.Year(), t.Month(), t.Day(), s.hour, s.min, 0, 0, s.loc)
	if !cand.After(t) {
		cand = time.Date(t.Year(), t.Month(), t.Day()+1, s.hour, s.min, 0, 0, s.loc)
	}
	return cand
}

// Run blocks until ctx is cancelled, firing Trigger at each scheduled time.
func (s *Scheduler) Run(ctx context.Context) {
	for {
		next := s.Next(s.now())
		wait := time.Until(next)
		s.log.Info("next scheduled sync", "at", next.Format(time.RFC3339), "in", wait.Round(time.Second).String())

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if err := s.fire("scheduled"); err != nil {
				s.log.Warn("scheduled sync not started", "err", err)
			}
		}
	}
}
