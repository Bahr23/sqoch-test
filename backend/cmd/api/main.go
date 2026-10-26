package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	delivery "sqoch-ticket/backend/internal/delivery/http"
	"sqoch-ticket/backend/internal/infrastructure/config"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"syscall"
	"time"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://127.0.0.1:" + cfg.Port + "/api/health")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("healthcheck returned %d", response.StatusCode)
		}
		return nil
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	initCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	db, err := sqlite.Open(initCtx, cfg.SQLitePath)
	if err != nil {
		cancel()
		return err
	}
	defer db.Close()
	if err := sqlite.Migrate(initCtx, db); err != nil {
		cancel()
		return err
	}
	cancel()
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           delivery.NewBookingHandler(usecase.NewHealth(db), usecase.NewBooking(sqlite.NewBookingRepository(db), cfg.DemoMode), logger),
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second,
		WriteTimeout: 10 * time.Second, IdleTimeout: 60 * time.Second,
	}
	errorsCh := make(chan error, 1)
	go func() { errorsCh <- server.ListenAndServe() }()
	logger.Info("API started", "port", cfg.Port)
	select {
	case err := <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			return err
		}
	}
	return nil
}
