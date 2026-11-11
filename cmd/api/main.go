package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"user-management-api/internal/adapters/auth"
	httpadapter "user-management-api/internal/adapters/http"
	"user-management-api/internal/adapters/repository/mongodb"
	"user-management-api/internal/application/userservice"
	"user-management-api/internal/infrastructure/config"
	"user-management-api/internal/infrastructure/database"
)

const (
	mongoTimeout    = 5 * time.Second
	shutdownTimeout = 10 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	client, err := database.Connect(ctx, cfg.MongoURI, mongoTimeout)
	if err != nil {
		return err
	}
	defer func() {
		disconnectCtx, cancel := context.WithTimeout(context.Background(), mongoTimeout)
		defer cancel()
		if err := client.Disconnect(disconnectCtx); err != nil {
			logger.Error("mongodb disconnect failed", "error", err)
		}
	}()
	logger.Info("connected to mongodb", "database", cfg.MongoDatabase)

	users := mongodb.NewUserRepository(client.Database(cfg.MongoDatabase))
	if err := users.EnsureIndexes(ctx); err != nil {
		return err
	}

	tokens, err := auth.NewJWTService([]byte(cfg.JWTSecret), cfg.JWTIssuer, cfg.JWTTTL, time.Now)
	if err != nil {
		return err
	}
	service := userservice.New(users, auth.NewBcryptHasher(cfg.BcryptCost), tokens, time.Now)
	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpadapter.NewHandler(service, userservice.NewAuthenticator(tokens, users), logger).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		serveErr <- server.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		stop()
	}

	logger.Info("shutting down", "timeout", shutdownTimeout.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server shutdown: %w", err)
	}
	logger.Info("http server stopped")
	return nil
}
