package grpc

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"user-management-api/internal/application/userservice"
)

var errUnauthenticated = status.Error(codes.Unauthenticated, "a valid bearer token is required")

func requireAuth(auth Authenticator, logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		token, ok := bearerToken(ctx)
		if !ok {
			return nil, errUnauthenticated
		}
		if _, err := auth.Authenticate(ctx, token); err != nil {
			if errors.Is(err, userservice.ErrUnauthenticated) {
				return nil, errUnauthenticated
			}
			logger.ErrorContext(ctx, "rpc failed", "method", info.FullMethod, "error", err)
			return nil, errInternal
		}
		return handler(ctx, req)
	}
}

func bearerToken(ctx context.Context) (string, bool) {
	values := metadata.ValueFromIncomingContext(ctx, "authorization")
	if len(values) != 1 {
		return "", false
	}
	scheme, token, found := strings.Cut(values[0], " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}
