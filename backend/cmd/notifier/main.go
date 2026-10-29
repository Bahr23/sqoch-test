package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sqoch-ticket/backend/internal/infrastructure/config"
	"sqoch-ticket/backend/internal/infrastructure/outbox"
	"sqoch-ticket/backend/internal/infrastructure/rabbit"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/notifications/infrastructure"
	"sqoch-ticket/backend/internal/notifications/usecase"
	"sync"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("notifier failed", "error", err)
		os.Exit(1)
	}
}
func run() error {
	cfg, err := config.LoadNotifier()
	if err != nil {
		return err
	}
	var level slog.Level
	if err = level.UnmarshalText([]byte(os.Getenv("LOG_LEVEL"))); err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	if len(os.Args) > 1 {
		if len(os.Args) != 2 || os.Args[1] != "healthcheck" {
			return fmt.Errorf("usage: notifier [healthcheck]")
		}
		client := http.Client{Timeout: 3 * time.Second}
		r, err := client.Get("http://127.0.0.1:" + cfg.Port + "/health")
		if err != nil {
			return err
		}
		defer r.Body.Close()
		if r.StatusCode != 200 {
			return fmt.Errorf("notifier unhealthy")
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	db, err := sqlite.Open(ctx, cfg.SQLitePath)
	if err != nil {
		return err
	}
	defer db.Close()
	mockDB, err := sqlite.Open(ctx, cfg.MockSQLitePath)
	if err != nil {
		return err
	}
	defer mockDB.Close()
	store := &infrastructure.Store{DB: db, ResultQueue: cfg.Messaging.Rabbit.Results}
	if err = store.Migrate(ctx); err != nil {
		return err
	}
	sender := &infrastructure.MockSender{DB: mockDB, Mode: cfg.MockFailureMode, Delay: cfg.MockDelay}
	if err = sender.Migrate(ctx); err != nil {
		return err
	}
	eligibility := &infrastructure.EligibilityClient{BaseURL: cfg.BackendURL, Token: cfg.Messaging.Token, Client: &http.Client{Timeout: cfg.Timeout}}
	worker := &usecase.Worker{Repository: store, Sender: sender, Eligibility: eligibility, Lease: cfg.Lease, Timeout: cfg.Timeout, RetryBase: cfg.RetryBase, Interval: cfg.Interval, MaxAttempts: cfg.MaxAttempts}
	var wg sync.WaitGroup
	wg.Add(3)
	go func() { defer wg.Done(); rabbit.RunPublisher(ctx, cfg.Messaging.Rabbit, outbox.Store{DB: db}, logger) }()
	go func() {
		defer wg.Done()
		rabbit.RunConsumer(ctx, cfg.Messaging.Rabbit, cfg.Messaging.Rabbit.Commands, store.Ingest, logger)
	}()
	go func() { defer wg.Done(); worker.Run(ctx, logger) }()
	defer func() { stop(); wg.Wait() }()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		healthCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if db.PingContext(healthCtx) != nil || mockDB.PingContext(healthCtx) != nil {
			w.WriteHeader(503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "sender": "mock"})
	})
	server := &http.Server{Addr: ":" + cfg.Port, Handler: mux, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.ListenAndServe() }()
	logger.Info("notifier started", "sender", "mock")
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err = server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
	}
	return nil
}
