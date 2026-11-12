package grpc

import (
	"context"
	"log/slog"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "user-management-api/gen/user/v1"
	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

type UserService interface {
	Register(ctx context.Context, in userservice.RegisterInput) (user.User, error)
	GetUser(ctx context.Context, id string) (user.User, error)
}

type Authenticator interface {
	Authenticate(ctx context.Context, token string) (string, error)
}

type userServer struct {
	userv1.UnimplementedUserServiceServer
	users  UserService
	logger *slog.Logger
}

func NewServer(users UserService, auth Authenticator, logger *slog.Logger) *grpc.Server {
	server := grpc.NewServer(grpc.UnaryInterceptor(requireAuth(auth, logger)))
	userv1.RegisterUserServiceServer(server, &userServer{users: users, logger: logger})
	return server
}

func (s *userServer) CreateUser(ctx context.Context, req *userv1.CreateUserRequest) (*userv1.CreateUserResponse, error) {
	created, err := s.users.Register(ctx, userservice.RegisterInput{
		Name:     req.GetName(),
		Email:    req.GetEmail(),
		Password: req.GetPassword(),
	})
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.CreateUserResponse{User: toProtoUser(created)}, nil
}

func (s *userServer) GetUser(ctx context.Context, req *userv1.GetUserRequest) (*userv1.GetUserResponse, error) {
	found, err := s.users.GetUser(ctx, req.GetId())
	if err != nil {
		return nil, s.toStatus(ctx, err)
	}
	return &userv1.GetUserResponse{User: toProtoUser(found)}, nil
}

func toProtoUser(u user.User) *userv1.User {
	return &userv1.User{
		Id:        u.ID,
		Name:      u.Name,
		Email:     u.Email,
		CreatedAt: timestamppb.New(u.CreatedAt),
	}
}
