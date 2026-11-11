package grpc

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/types/known/timestamppb"

	userv1 "user-management-api/gen/user/v1"
	"user-management-api/internal/adapters/auth"
	"user-management-api/internal/application/userservice"
	"user-management-api/internal/domain/user"
)

const (
	validToken = "header.payload.signature"
	callerID   = "6730f1c2a1b2c3d4e5f60718"
)

var stored = user.User{
	ID:           "6730f1c2a1b2c3d4e5f60719",
	Name:         "Bob Jones",
	Email:        "bob@example.com",
	PasswordHash: "$2a$12$stored-hash-value",
	CreatedAt:    time.Date(2026, time.November, 11, 10, 0, 0, 123_000_000, time.UTC),
}

func TestCreateUser(t *testing.T) {
	t.Parallel()

	request := &userv1.CreateUserRequest{Name: "Bob Jones", Email: "bob@example.com", Password: "correct horse"}
	accept := func(context.Context, string) (string, error) { return callerID, nil }
	returns := func(u user.User, err error) func(context.Context, userservice.RegisterInput) (user.User, error) {
		return func(context.Context, userservice.RegisterInput) (user.User, error) { return u, err }
	}

	tests := []struct {
		name           string
		authorization  []string
		authenticate   func(context.Context, string) (string, error)
		register       func(context.Context, userservice.RegisterInput) (user.User, error)
		wantCode       codes.Code
		wantMessage    string
		wantViolations []string
		wantUser       *userv1.User
		wantRegister   bool
		wantLog        string
	}{
		{
			name:          "created",
			authorization: []string{"Bearer " + validToken},
			authenticate:  accept,
			register:      returns(stored, nil),
			wantCode:      codes.OK,
			wantUser:      protoUser(stored),
			wantRegister:  true,
		},
		{
			name:          "validation errors carry field details",
			authorization: []string{"Bearer " + validToken},
			authenticate:  accept,
			register: returns(user.User{}, &user.ValidationError{Fields: []user.FieldError{
				{Field: "email", Message: "must be a valid email address"},
				{Field: "password", Message: "must be at least 8 characters"},
			}}),
			wantCode:       codes.InvalidArgument,
			wantMessage:    "request validation failed",
			wantViolations: []string{"email: must be a valid email address", "password: must be at least 8 characters"},
			wantRegister:   true,
		},
		{
			name:          "email already registered",
			authorization: []string{"Bearer " + validToken},
			authenticate:  accept,
			register:      returns(user.User{}, fmt.Errorf("register user: %w", user.ErrEmailAlreadyExists)),
			wantCode:      codes.AlreadyExists,
			wantMessage:   "email is already registered",
			wantRegister:  true,
		},
		{
			name:          "internal error is hidden from the client and logged",
			authorization: []string{"Bearer " + validToken},
			authenticate:  accept,
			register:      returns(user.User{}, errors.New("register user: mongodb: create user: not primary")),
			wantCode:      codes.Internal,
			wantMessage:   "internal server error",
			wantRegister:  true,
			wantLog:       "not primary",
		},
		{
			name:        "no token",
			wantCode:    codes.Unauthenticated,
			wantMessage: "a valid bearer token is required",
		},
		{
			name:          "rejected token",
			authorization: []string{"Bearer " + validToken},
			authenticate: func(context.Context, string) (string, error) {
				return "", fmt.Errorf("%w: jwt: invalid token", userservice.ErrUnauthenticated)
			},
			wantCode:    codes.Unauthenticated,
			wantMessage: "a valid bearer token is required",
		},
		{
			name:          "authentication lookup failure is internal, not unauthenticated",
			authorization: []string{"Bearer " + validToken},
			authenticate: func(context.Context, string) (string, error) {
				return "", errors.New("authenticate: mongodb: find user by id: server selection timeout")
			},
			wantCode:    codes.Internal,
			wantMessage: "internal server error",
			wantLog:     "server selection timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{RegisterFunc: tt.register}
			authenticator := &AuthenticatorMock{AuthenticateFunc: tt.authenticate}
			logs := &lockedBuffer{}
			client := newTestClient(t, service, authenticator, logs)

			resp, err := client.CreateUser(withAuthorization(t.Context(), tt.authorization...), request)

			assertStatus(t, err, tt.wantCode, tt.wantMessage, tt.wantViolations)
			if tt.wantUser != nil && !sameProtoUser(resp.GetUser(), tt.wantUser) {
				t.Errorf("user = %v, want %v", resp.GetUser(), tt.wantUser)
			}
			calls := service.RegisterCalls()
			if !tt.wantRegister {
				if len(calls) != 0 {
					t.Errorf("Register calls = %d, want 0", len(calls))
				}
			} else if len(calls) != 1 || calls[0].In != (userservice.RegisterInput{Name: "Bob Jones", Email: "bob@example.com", Password: "correct horse"}) {
				t.Errorf("Register calls = %+v, want one call with the request fields", calls)
			}
			assertLogs(t, logs, tt.wantLog, "correct horse")
		})
	}
}

func TestGetUser(t *testing.T) {
	t.Parallel()

	accept := func(context.Context, string) (string, error) { return callerID, nil }
	returns := func(u user.User, err error) func(context.Context, string) (user.User, error) {
		return func(context.Context, string) (user.User, error) { return u, err }
	}

	tests := []struct {
		name          string
		id            string
		authorization []string
		getUser       func(context.Context, string) (user.User, error)
		wantCode      codes.Code
		wantMessage   string
		wantUser      *userv1.User
		wantLookup    bool
	}{
		{
			name:          "existing user",
			id:            stored.ID,
			authorization: []string{"Bearer " + validToken},
			getUser:       returns(stored, nil),
			wantCode:      codes.OK,
			wantUser:      protoUser(stored),
			wantLookup:    true,
		},
		{
			name:          "scheme is case-insensitive",
			id:            stored.ID,
			authorization: []string{"bearer " + validToken},
			getUser:       returns(stored, nil),
			wantCode:      codes.OK,
			wantUser:      protoUser(stored),
			wantLookup:    true,
		},
		{
			name:          "unknown id",
			id:            "6730f1c2a1b2c3d4e5f60000",
			authorization: []string{"Bearer " + validToken},
			getUser:       returns(user.User{}, fmt.Errorf("get user: %w", user.ErrNotFound)),
			wantCode:      codes.NotFound,
			wantMessage:   "user not found",
			wantLookup:    true,
		},
		{
			name:          "malformed id",
			id:            "not-an-id",
			authorization: []string{"Bearer " + validToken},
			getUser:       returns(user.User{}, fmt.Errorf("get user: %w", user.ErrInvalidID)),
			wantCode:      codes.InvalidArgument,
			wantMessage:   "id must be a 24-character hexadecimal string",
			wantLookup:    true,
		},
		{
			name:          "basic credentials",
			id:            stored.ID,
			authorization: []string{"Basic YWxpY2U6c2VjcmV0"},
			wantCode:      codes.Unauthenticated,
			wantMessage:   "a valid bearer token is required",
		},
		{
			name:          "scheme without a token",
			id:            stored.ID,
			authorization: []string{"Bearer   "},
			wantCode:      codes.Unauthenticated,
			wantMessage:   "a valid bearer token is required",
		},
		{
			name:          "two authorization values are ambiguous",
			id:            stored.ID,
			authorization: []string{"Bearer " + validToken, "Bearer " + validToken},
			wantCode:      codes.Unauthenticated,
			wantMessage:   "a valid bearer token is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &UserServiceMock{GetUserFunc: tt.getUser}
			authenticator := &AuthenticatorMock{AuthenticateFunc: accept}
			client := newTestClient(t, service, authenticator, &lockedBuffer{})

			resp, err := client.GetUser(withAuthorization(t.Context(), tt.authorization...), &userv1.GetUserRequest{Id: tt.id})

			assertStatus(t, err, tt.wantCode, tt.wantMessage, nil)
			if tt.wantUser != nil && !sameProtoUser(resp.GetUser(), tt.wantUser) {
				t.Errorf("user = %v, want %v", resp.GetUser(), tt.wantUser)
			}
			lookups := service.GetUserCalls()
			switch {
			case !tt.wantLookup && len(lookups) != 0:
				t.Errorf("GetUser calls = %d, want 0", len(lookups))
			case tt.wantLookup && (len(lookups) != 1 || lookups[0].ID != tt.id):
				t.Errorf("GetUser calls = %+v, want one call with %q", lookups, tt.id)
			}
			if auths := authenticator.AuthenticateCalls(); tt.wantLookup && (len(auths) != 1 || auths[0].Token != validToken) {
				t.Errorf("Authenticate calls = %+v, want one call with the bearer token", auths)
			}
		})
	}
}

type finderFunc func(ctx context.Context, id string) (user.User, error)

func (f finderFunc) FindByID(ctx context.Context, id string) (user.User, error) { return f(ctx, id) }

func TestGetUser_WithRealAuthentication(t *testing.T) {
	t.Parallel()

	const secret = "test-secret-that-is-at-least-32-bytes-long"
	now := time.Now()
	newJWT := func(at time.Time) *auth.JWTService {
		service, err := auth.NewJWTService([]byte(secret), "user-management-api", time.Hour, func() time.Time { return at })
		if err != nil {
			t.Fatalf("NewJWTService() error = %v", err)
		}
		return service
	}
	verifier := newJWT(now)
	exists := finderFunc(func(context.Context, string) (user.User, error) { return stored, nil })
	deleted := finderFunc(func(context.Context, string) (user.User, error) { return user.User{}, user.ErrNotFound })

	tests := []struct {
		name     string
		issuer   *auth.JWTService
		users    finderFunc
		wantCode codes.Code
	}{
		{name: "token from login", issuer: verifier, users: exists, wantCode: codes.OK},
		{name: "expired token", issuer: newJWT(now.Add(-2 * time.Hour)), users: exists, wantCode: codes.Unauthenticated},
		{name: "valid token whose user was deleted", issuer: verifier, users: deleted, wantCode: codes.Unauthenticated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			token, err := tt.issuer.Issue(callerID)
			if err != nil {
				t.Fatalf("Issue() error = %v", err)
			}
			service := &UserServiceMock{GetUserFunc: func(context.Context, string) (user.User, error) { return stored, nil }}
			client := newTestClient(t, service, userservice.NewAuthenticator(verifier, tt.users), &lockedBuffer{})

			_, err = client.GetUser(withAuthorization(t.Context(), "Bearer "+token.Value), &userv1.GetUserRequest{Id: stored.ID})

			if got := status.Code(err); got != tt.wantCode {
				t.Errorf("code = %v, want %v (err %v)", got, tt.wantCode, err)
			}
		})
	}
}

func newTestClient(t *testing.T, users UserService, authenticator Authenticator, logs *lockedBuffer) userv1.UserServiceClient {
	t.Helper()

	listener := bufconn.Listen(1 << 20)
	server := NewServer(users, authenticator, slog.New(slog.NewJSONHandler(logs, nil)))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return userv1.NewUserServiceClient(conn)
}

func withAuthorization(ctx context.Context, values ...string) context.Context {
	for _, v := range values {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", v)
	}
	return ctx
}

func protoUser(u user.User) *userv1.User {
	return &userv1.User{Id: u.ID, Name: u.Name, Email: u.Email, CreatedAt: timestamppb.New(u.CreatedAt)}
}

func sameProtoUser(got, want *userv1.User) bool {
	return got.GetId() == want.GetId() &&
		got.GetName() == want.GetName() &&
		got.GetEmail() == want.GetEmail() &&
		got.GetCreatedAt() != nil &&
		got.GetCreatedAt().AsTime().Equal(want.GetCreatedAt().AsTime())
}

func assertStatus(t *testing.T, err error, wantCode codes.Code, wantMessage string, wantViolations []string) {
	t.Helper()

	st := status.Convert(err)
	if st.Code() != wantCode {
		t.Fatalf("code = %v, want %v (err %v)", st.Code(), wantCode, err)
	}
	if wantCode == codes.OK {
		return
	}
	if st.Message() != wantMessage {
		t.Errorf("message = %q, want %q", st.Message(), wantMessage)
	}
	var violations []string
	for _, detail := range st.Details() {
		if br, ok := detail.(*errdetails.BadRequest); ok {
			for _, v := range br.GetFieldViolations() {
				violations = append(violations, v.GetField()+": "+v.GetDescription())
			}
		}
	}
	if !slices.Equal(violations, wantViolations) {
		t.Errorf("field violations = %v, want %v", violations, wantViolations)
	}
}

func assertLogs(t *testing.T, logs *lockedBuffer, wantLog string, secrets ...string) {
	t.Helper()

	got := logs.String()
	if wantLog == "" && got != "" {
		t.Errorf("unexpected log output: %s", got)
	}
	if wantLog != "" && !strings.Contains(got, wantLog) {
		t.Errorf("log = %s, want it to contain %q", got, wantLog)
	}
	for _, secret := range secrets {
		if strings.Contains(got, secret) {
			t.Errorf("log contains %q: %s", secret, got)
		}
	}
}

type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
