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
	adminauth "sqoch-ticket/backend/internal/infrastructure/auth"
	"sqoch-ticket/backend/internal/infrastructure/config"
	"sqoch-ticket/backend/internal/infrastructure/openrouter"
	"sqoch-ticket/backend/internal/infrastructure/outbox"
	"sqoch-ticket/backend/internal/infrastructure/rabbit"
	"sqoch-ticket/backend/internal/infrastructure/sqlite"
	"sqoch-ticket/backend/internal/usecase"
	"sync"
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
	seedOrders := len(os.Args) == 2 && os.Args[1] == "seed-orders"
	if seedOrders && !cfg.DemoMode {
		return fmt.Errorf("seed-orders requires DEMO_MODE=true")
	}
	if len(os.Args) > 1 && !seedOrders && (len(os.Args) != 2 || os.Args[1] != "healthcheck") {
		return fmt.Errorf("usage: api [healthcheck|seed-orders]")
	}
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
	if seedOrders {
		defer cancel()
		report, err := sqlite.SeedDemoOrders(initCtx, db, time.Now())
		if err != nil {
			return err
		}
		logger.Info("demo orders ready", "report", report)
		return nil
	}
	sessions, err := sqlite.NewAdminSessions(initCtx, db)
	if err != nil {
		cancel()
		return err
	}
	cancel()
	passwords, err := adminauth.NewPassword(cfg.AdminPassword)
	if err != nil {
		return err
	}
	auth := usecase.NewAuth(cfg.AdminEmail, passwords, adminauth.NewTokens(cfg.JWTSecret), sessions, cfg.JWTTTL)
	messaging, err := config.LoadMessaging()
	if err != nil {
		return err
	}
	mailingsRepo := sqlite.NewMailingRepository(db, messaging.Rabbit.Commands)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() { defer workers.Done(); rabbit.RunPublisher(ctx, messaging.Rabbit, outbox.Store{DB: db}, logger) }()
	go func() {
		defer workers.Done()
		rabbit.RunConsumer(ctx, messaging.Rabbit, messaging.Rabbit.Results, mailingsRepo.ApplyResult, logger)
	}()
	defer func() { stop(); workers.Wait() }()
	assistantConfig, err := config.LoadAssistant()
	if err != nil {
		return err
	}
	agent := &usecase.Assistant{Repo: sqlite.NewAssistantRepository(db), Events: usecase.NewEvents(sqlite.NewBookingRepository(db)), Mailings: usecase.NewMailings(mailingsRepo), Enabled: assistantConfig.Key != "" && assistantConfig.Model != "", ModelName: assistantConfig.Model, Timeout: assistantConfig.Timeout, MaxSteps: assistantConfig.MaxSteps, Logger: logger, Model: &openrouter.Client{Key: assistantConfig.Key, Model: assistantConfig.Model, Endpoint: "https://openrouter.ai/api/v1/chat/completions", MaxTokens: assistantConfig.MaxTokens, HTTP: &http.Client{Timeout: assistantConfig.Timeout}}}
	workers.Add(1)
	go func() { defer workers.Done(); agent.Run(ctx) }()
	router := http.NewServeMux()
	organizerRoutes := delivery.NewOrganizerRoutes(usecase.NewEvents(sqlite.NewBookingRepository(db)), usecase.NewMailings(mailingsRepo), logger)
	adminRoutes := http.NewServeMux()
	adminRoutes.Handle("/api/admin/assistant/", delivery.NewAssistantHandler(agent, logger))
	adminRoutes.Handle("/api/admin/", organizerRoutes)
	router.Handle("/api/admin/", delivery.NewAdminHandler(auth, cfg.AdminOrigin, logger, adminRoutes))
	router.Handle("/internal/", delivery.NewEligibilityHandler(mailingsRepo, messaging.Token, logger))
	router.Handle("/", delivery.NewBookingHandler(usecase.NewHealth(db), usecase.NewBooking(sqlite.NewBookingRepository(db), cfg.DemoMode), logger))
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
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
