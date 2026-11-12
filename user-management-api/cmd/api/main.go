package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"

	"user-management-api/internal/adapters/auth"
	grpcadapter "user-management-api/internal/adapters/grpc"
	httpadapter "user-management-api/internal/adapters/http"
	"user-management-api/internal/adapters/repository/mongodb"
	"user-management-api/internal/adapters/worker"
	"user-management-api/internal/application/userservice"
	"user-management-api/internal/infrastructure/config"
	"user-management-api/internal/infrastructure/database"
)

const (
	mongoTimeout      = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
	userCountInterval = 10 * time.Second
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(context.Background(), config.HTTPAddr(os.Getenv)); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	if err := run(ctx, logger, os.Getenv); err != nil {
		logger.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, getenv func(string) string) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}

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
	authenticator := userservice.NewAuthenticator(tokens, users)
	server := &http.Server{
		Handler:           httpadapter.NewHandler(service, authenticator, database.NewHealth(client), logger).Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}

	grpcServer := grpcadapter.NewServer(service, authenticator, logger)
	counter := worker.NewUserCountLogger(service, userCountInterval, logger)

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.HTTPAddr)
	if err != nil {
		return fmt.Errorf("http server: %w", err)
	}
	grpcListener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.GRPCAddr)
	if err != nil {
		return errors.Join(fmt.Errorf("grpc server: %w", err), listener.Close())
	}
	logger.Info("http server listening", "addr", listener.Addr().String())
	logger.Info("grpc server listening", "addr", grpcListener.Addr().String())

	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() error {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	})
	group.Go(func() error {
		return serveGRPC(grpcServer, grpcListener)
	})
	group.Go(func() error {
		counter.Run(groupCtx)
		return nil
	})
	group.Go(func() error {
		<-groupCtx.Done()
		logger.Info("shutting down", "timeout", shutdownTimeout.String())
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return shutdown(shutdownCtx, logger, server, grpcServer)
	})
	return group.Wait()
}

func serveGRPC(server *grpc.Server, listener net.Listener) error {
	if err := server.Serve(listener); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
		return fmt.Errorf("grpc server: %w", err)
	}
	return nil
}

func shutdown(ctx context.Context, logger *slog.Logger, httpServer *http.Server, grpcServer *grpc.Server) error {
	var httpErr, grpcErr error
	var stopping sync.WaitGroup
	stopping.Go(func() {
		if httpErr = stopHTTP(ctx, httpServer); httpErr == nil {
			logger.Info("http server stopped")
		}
	})
	stopping.Go(func() {
		if grpcErr = stopGRPC(ctx, grpcServer); grpcErr == nil {
			logger.Info("grpc server stopped")
		}
	})
	stopping.Wait()
	return errors.Join(httpErr, grpcErr)
}

func stopHTTP(ctx context.Context, server *http.Server) error {
	if err := server.Shutdown(ctx); err != nil {
		return errors.Join(fmt.Errorf("http server shutdown: %w", err), server.Close())
	}
	return nil
}

func stopGRPC(ctx context.Context, server *grpc.Server) error {
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		server.Stop()
		<-stopped
		return fmt.Errorf("grpc server shutdown: %w", ctx.Err())
	}
}
