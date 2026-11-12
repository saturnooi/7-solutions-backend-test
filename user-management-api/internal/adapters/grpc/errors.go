package grpc

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"user-management-api/internal/domain/user"
)

func (s *userServer) toStatus(ctx context.Context, err error) error {
	if verr, ok := errors.AsType[*user.ValidationError](err); ok {
		return validationStatus(verr)
	}
	switch {
	case errors.Is(err, user.ErrInvalidID):
		return status.Error(codes.InvalidArgument, "id must be a 24-character lowercase hexadecimal string")
	case errors.Is(err, user.ErrNotFound):
		return status.Error(codes.NotFound, "user not found")
	case errors.Is(err, user.ErrEmailAlreadyExists):
		return status.Error(codes.AlreadyExists, "email is already registered")
	}

	method, _ := grpc.Method(ctx)
	return unexpected(ctx, s.logger, method, err)
}

func unexpected(ctx context.Context, logger *slog.Logger, method string, err error) error {
	if errors.Is(err, ctx.Err()) {
		logger.InfoContext(ctx, "rpc canceled", "method", method, "error", err)
		return status.FromContextError(ctx.Err()).Err()
	}
	logger.ErrorContext(ctx, "rpc failed", "method", method, "error", err)
	return errInternal
}

func validationStatus(verr *user.ValidationError) error {
	violations := make([]*errdetails.BadRequest_FieldViolation, len(verr.Fields))
	for i, f := range verr.Fields {
		violations[i] = &errdetails.BadRequest_FieldViolation{Field: f.Field, Description: f.Message}
	}
	st := status.New(codes.InvalidArgument, "request validation failed")
	detailed, err := st.WithDetails(&errdetails.BadRequest{FieldViolations: violations})
	if err != nil {
		return st.Err()
	}
	return detailed.Err()
}

var errInternal = status.Error(codes.Internal, "internal server error")
