// Command eetmeter-sync keeps the recipe collections of two Mijn Eetmeter
// accounts in sync. It serves a small HTTP API and runs a daily reconciliation.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mortenzwarenstein/eetmeter-sync/internal/config"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/eetmeter"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/httpapi"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/scheduler"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/store"
	"github.com/mortenzwarenstein/eetmeter-sync/internal/syncengine"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		// Logger not configured yet; use the default.
		return err
	}

	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(log)
	log.Info("starting eetmeter-sync", "config", cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	if err := st.Migrate(ctx); err != nil {
		return err
	}
	if err := st.ClearStaleRunningRuns(ctx); err != nil {
		return err
	}
	if err := st.UpsertAccount(ctx, store.Account{ID: "a", Label: cfg.AccountA.Label, Email: cfg.AccountA.Email}); err != nil {
		return err
	}
	if err := st.UpsertAccount(ctx, store.Account{ID: "b", Label: cfg.AccountB.Label, Email: cfg.AccountB.Email}); err != nil {
		return err
	}

	factory := func(ac syncengine.AccountConfig) syncengine.Client {
		return eetmeter.New(ac.Email, ac.Password, eetmeter.Options{
			BaseURL:    cfg.Eetmeter.BaseURL,
			AppVersion: cfg.Eetmeter.AppVersion,
			Platform:   cfg.Eetmeter.Platform,
			Token:      ac.Token,
			DeviceID:   ac.DeviceID,
		})
	}
	acct := func(a config.Account) syncengine.AccountConfig {
		return syncengine.AccountConfig{
			Label: a.Label, Email: a.Email, Password: a.Password,
			Token: a.Token, DeviceID: a.DeviceID,
		}
	}
	engine := syncengine.New(st, acct(cfg.AccountA), acct(cfg.AccountB),
		factory, log, 10*time.Minute, cfg.DryRun)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.New(engine, httpapi.NewStore(st), cfg.APIToken, log),
		ReadHeaderTimeout: 10 * time.Second,
	}

	// HTTP server.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Daily scheduler.
	hour, min := cfg.DailyRun()
	sched := scheduler.New(hour, min, cfg.Location(), func(reason string) error {
		_, _, err := engine.Trigger(reason)
		return err
	}, log)
	go sched.Run(ctx)

	if cfg.RunOnStartup {
		if _, _, err := engine.Trigger("manual"); err != nil {
			log.Warn("startup sync not started", "err", err)
		}
	}

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
