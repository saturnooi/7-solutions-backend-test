//go:build integration

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	userv1 "user-management-api/gen/user/v1"
	"user-management-api/internal/infrastructure/database"
)

const (
	e2eSecret   = "e2e-secret-that-is-at-least-32-bytes-long"
	e2ePassword = "correct horse"
)

func TestRun_EndToEnd(t *testing.T) {
	app := startApp(t)
	step := func(name string, f func(t *testing.T)) {
		t.Helper()
		if !t.Run(name, f) {
			t.FailNow()
		}
	}
	var aliceID, aliceToken, bobID, bobToken, carolID string

	step("health check passes", func(t *testing.T) {
		app.expect(t, http.MethodGet, "/healthz", "", nil, http.StatusOK, `{"status":"ok"}`)
	})
	step("alice registers", func(t *testing.T) {
		body := app.expect(t, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
			"name": "Alice Smith", "email": " Alice@Example.com ", "password": e2ePassword,
		}, http.StatusCreated, "")
		aliceID = body["id"].(string)
		if body["email"] != "alice@example.com" {
			t.Errorf("email = %v, want the normalized address", body["email"])
		}
	})
	step("registering the same email again is a conflict", func(t *testing.T) {
		app.expect(t, http.MethodPost, "/api/v1/auth/register", "", map[string]string{
			"name": "Alice Again", "email": "alice@example.com", "password": e2ePassword,
		}, http.StatusConflict, `{"error":{"code":"email_already_exists","message":"email is already registered"}}`)
	})
	step("a wrong password is rejected", func(t *testing.T) {
		app.expect(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email": "alice@example.com", "password": "wrong password",
		}, http.StatusUnauthorized, `{"error":{"code":"invalid_credentials","message":"invalid email or password"}}`)
	})
	step("alice logs in", func(t *testing.T) {
		aliceToken = app.login(t, "alice@example.com")
	})
	step("protected routes need a token", func(t *testing.T) {
		app.expect(t, http.MethodGet, "/api/v1/users", "", nil, http.StatusUnauthorized,
			`{"error":{"code":"unauthorized","message":"a valid bearer token is required"}}`)
	})
	step("alice creates bob", func(t *testing.T) {
		body := app.expect(t, http.MethodPost, "/api/v1/users", aliceToken, map[string]string{
			"name": "Bob Jones", "email": "bob@example.com", "password": e2ePassword,
		}, http.StatusCreated, "")
		bobID = body["id"].(string)
		bobToken = app.login(t, "bob@example.com")
	})
	step("the list is newest first", func(t *testing.T) {
		body := app.expect(t, http.MethodGet, "/api/v1/users?limit=10", aliceToken, nil, http.StatusOK, "")
		if got := userIDs(body); strings.Join(got, ",") != bobID+","+aliceID {
			t.Errorf("listed ids = %v, want [%s %s]", got, bobID, aliceID)
		}
	})
	step("alice renames herself", func(t *testing.T) {
		app.expect(t, http.MethodPatch, "/api/v1/users/"+aliceID, aliceToken, map[string]string{"name": "Alice Cooper"}, http.StatusOK, "")
		body := app.expect(t, http.MethodGet, "/api/v1/users/"+aliceID, bobToken, nil, http.StatusOK, "")
		if body["name"] != "Alice Cooper" {
			t.Errorf("name = %v, want Alice Cooper", body["name"])
		}
	})
	step("alice cannot change or delete bob", func(t *testing.T) {
		forbidden := `{"error":{"code":"forbidden","message":"you may only change your own account"}}`
		app.expect(t, http.MethodPatch, "/api/v1/users/"+bobID, aliceToken, map[string]string{"name": "Hacked"}, http.StatusForbidden, forbidden)
		app.expect(t, http.MethodDelete, "/api/v1/users/"+bobID, aliceToken, nil, http.StatusForbidden, forbidden)
	})
	step("alice cannot take bob's email", func(t *testing.T) {
		app.expect(t, http.MethodPatch, "/api/v1/users/"+aliceID, aliceToken, map[string]string{"email": "bob@example.com"}, http.StatusConflict,
			`{"error":{"code":"email_already_exists","message":"email is already registered"}}`)
	})
	step("gRPC reads a user with a token from the REST login", func(t *testing.T) {
		resp, err := app.grpc.GetUser(withToken(t.Context(), aliceToken), &userv1.GetUserRequest{Id: bobID})
		if err != nil {
			t.Fatalf("GetUser() error = %v", err)
		}
		if resp.GetUser().GetName() != "Bob Jones" || resp.GetUser().GetEmail() != "bob@example.com" {
			t.Errorf("GetUser() = %v, want Bob Jones", resp.GetUser())
		}
	})
	step("a user created over gRPC is visible over REST", func(t *testing.T) {
		resp, err := app.grpc.CreateUser(withToken(t.Context(), bobToken), &userv1.CreateUserRequest{
			Name: "Carol White", Email: "carol@example.com", Password: e2ePassword,
		})
		if err != nil {
			t.Fatalf("CreateUser() error = %v", err)
		}
		carolID = resp.GetUser().GetId()
		body := app.expect(t, http.MethodGet, "/api/v1/users/"+carolID, aliceToken, nil, http.StatusOK, "")
		if body["email"] != "carol@example.com" {
			t.Errorf("REST email = %v, want carol@example.com", body["email"])
		}
	})
	step("gRPC enforces the same rules", func(t *testing.T) {
		_, err := app.grpc.CreateUser(withToken(t.Context(), bobToken), &userv1.CreateUserRequest{
			Name: "Carol Again", Email: "CAROL@example.com", Password: e2ePassword,
		})
		if status.Code(err) != codes.AlreadyExists {
			t.Errorf("duplicate CreateUser() code = %v, want AlreadyExists", status.Code(err))
		}
		_, err = app.grpc.GetUser(t.Context(), &userv1.GetUserRequest{Id: bobID})
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("GetUser() without a token code = %v, want Unauthenticated", status.Code(err))
		}
	})
	step("alice deletes herself", func(t *testing.T) {
		app.expect(t, http.MethodDelete, "/api/v1/users/"+aliceID, aliceToken, nil, http.StatusNoContent, "")
	})
	step("alice's token stops working at once", func(t *testing.T) {
		app.expect(t, http.MethodGet, "/api/v1/users", aliceToken, nil, http.StatusUnauthorized, "")
		_, err := app.grpc.GetUser(withToken(t.Context(), aliceToken), &userv1.GetUserRequest{Id: bobID})
		if status.Code(err) != codes.Unauthenticated {
			t.Errorf("gRPC with a deleted user's token code = %v, want Unauthenticated", status.Code(err))
		}
		app.expect(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
			"email": "alice@example.com", "password": e2ePassword,
		}, http.StatusUnauthorized, "")
	})
	step("others see that alice is gone", func(t *testing.T) {
		app.expect(t, http.MethodGet, "/api/v1/users/"+aliceID, bobToken, nil, http.StatusNotFound,
			`{"error":{"code":"user_not_found","message":"user not found"}}`)
	})
	step("the api shuts down gracefully", func(t *testing.T) {
		if err := app.stop(); err != nil {
			t.Fatalf("run() error = %v, want nil after cancellation", err)
		}
		logs := app.logs.String()
		for _, msg := range []string{"user count worker stopped", "http server stopped", "grpc server stopped"} {
			if !strings.Contains(logs, `"msg":"`+msg+`"`) {
				t.Errorf("logs do not contain %q", msg)
			}
		}
	})
	step("logs hold no errors, passwords or tokens", func(t *testing.T) {
		logs := app.logs.String()
		for _, secret := range []string{`"level":"ERROR"`, e2ePassword, aliceToken, bobToken, e2eSecret} {
			if strings.Contains(logs, secret) {
				t.Errorf("logs contain %q", secret)
			}
		}
	})
}

func TestRun_StartupFailures(t *testing.T) {
	t.Parallel()

	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	occupied := func(t *testing.T) string {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("occupy a port: %v", err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		return listener.Addr().String()
	}
	free := func(t *testing.T) string {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("find a free port: %v", err)
		}
		addr := listener.Addr().String()
		_ = listener.Close()
		return addr
	}

	tests := []struct {
		name             string
		env              func(t *testing.T) map[string]string
		wantPrefix       string
		wantHTTPReleased bool
	}{
		{
			name:       "invalid configuration",
			env:        func(*testing.T) map[string]string { return map[string]string{"MONGO_URI": uri} },
			wantPrefix: "config: JWT_SECRET is required",
		},
		{
			name: "database unreachable",
			env: func(*testing.T) map[string]string {
				return map[string]string{"MONGO_URI": "mongodb://127.0.0.1:1/?serverSelectionTimeoutMS=300", "JWT_SECRET": e2eSecret}
			},
			wantPrefix: "mongodb: ping:",
		},
		{
			name: "HTTP port taken",
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MONGO_URI": uri, "JWT_SECRET": e2eSecret, "HTTP_ADDR": occupied(t), "GRPC_ADDR": "127.0.0.1:0"}
			},
			wantPrefix: "http server: listen tcp",
		},
		{
			name: "gRPC port taken",
			env: func(t *testing.T) map[string]string {
				return map[string]string{"MONGO_URI": uri, "JWT_SECRET": e2eSecret, "HTTP_ADDR": free(t), "GRPC_ADDR": occupied(t)}
			},
			wantPrefix:       "grpc server: listen tcp",
			wantHTTPReleased: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			env := tt.env(t)
			env["MONGO_DATABASE"] = "user_management_e2e_" + bson.NewObjectID().Hex()
			t.Cleanup(func() { dropDatabase(t, uri, env["MONGO_DATABASE"]) })
			logs := newNotifyingBuffer()

			err := run(t.Context(), slog.New(slog.NewJSONHandler(logs, nil)), func(key string) string { return env[key] })

			if err == nil || !strings.HasPrefix(err.Error(), tt.wantPrefix) {
				t.Fatalf("run() error = %v, want prefix %q", err, tt.wantPrefix)
			}
			if strings.Contains(logs.String(), "listening") {
				t.Errorf("a server reported listening although startup failed: %s", logs.String())
			}
			if tt.wantHTTPReleased {
				listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", env["HTTP_ADDR"])
				if err != nil {
					t.Fatalf("HTTP port %s was not released after the gRPC listener failed: %v", env["HTTP_ADDR"], err)
				}
				_ = listener.Close()
			}
		})
	}
}

type app struct {
	baseURL string
	grpc    userv1.UserServiceClient
	logs    *notifyingBuffer
	stop    func() error
}

func startApp(t *testing.T) *app {
	t.Helper()

	uri := os.Getenv("MONGO_TEST_URI")
	if uri == "" {
		uri = "mongodb://localhost:27017"
	}
	dbName := "user_management_e2e_" + bson.NewObjectID().Hex()
	env := map[string]string{
		"HTTP_ADDR":      "127.0.0.1:0",
		"GRPC_ADDR":      "127.0.0.1:0",
		"MONGO_URI":      uri,
		"MONGO_DATABASE": dbName,
		"JWT_SECRET":     e2eSecret,
		"BCRYPT_COST":    "10",
	}
	t.Cleanup(func() { dropDatabase(t, uri, dbName) })

	logs := newNotifyingBuffer()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, slog.New(slog.NewJSONHandler(logs, nil)), func(key string) string { return env[key] })
	}()

	var stopOnce sync.Once
	var stopErr error
	stop := func() error {
		stopOnce.Do(func() {
			cancel()
			select {
			case stopErr = <-done:
			case <-time.After(15 * time.Second):
				stopErr = errors.New("run did not return within 15 seconds of cancellation")
			}
		})
		return stopErr
	}
	t.Cleanup(func() { _ = stop() })

	httpAddr := logs.waitFor(t, "http server listening", done)["addr"].(string)
	grpcAddr := logs.waitFor(t, "grpc server listening", done)["addr"].(string)
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc.NewClient() error = %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return &app{baseURL: "http://" + httpAddr, grpc: userv1.NewUserServiceClient(conn), logs: logs, stop: stop}
}

func (a *app) expect(t *testing.T, method, path, token string, body any, wantStatus int, wantBody string) map[string]any {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(t.Context(), method, a.baseURL+path, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}

	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d (body %s)", method, path, resp.StatusCode, wantStatus, raw)
	}
	if wantBody != "" && strings.TrimSpace(string(raw)) != wantBody {
		t.Errorf("%s %s body = %s, want %s", method, path, raw, wantBody)
	}
	if strings.Contains(string(raw), `"password"`) || strings.Contains(string(raw), `"password_hash"`) || strings.Contains(string(raw), "$2a$") {
		t.Errorf("%s %s response exposes a password or hash: %s", method, path, raw)
	}
	var decoded map[string]any
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &decoded); err != nil {
			t.Fatalf("%s %s response is not JSON: %s", method, path, raw)
		}
	}
	return decoded
}

func (a *app) login(t *testing.T, email string) string {
	t.Helper()

	body := a.expect(t, http.MethodPost, "/api/v1/auth/login", "", map[string]string{
		"email": email, "password": e2ePassword,
	}, http.StatusOK, "")
	token, ok := body["access_token"].(string)
	if !ok || token == "" || body["token_type"] != "Bearer" {
		t.Fatalf("login response = %v, want a bearer token", body)
	}
	return token
}

func withToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
}

func userIDs(body map[string]any) []string {
	users, _ := body["users"].([]any)
	ids := make([]string, 0, len(users))
	for _, u := range users {
		if m, ok := u.(map[string]any); ok {
			ids = append(ids, m["id"].(string))
		}
	}
	return ids
}

func dropDatabase(t *testing.T, uri, name string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := database.Connect(ctx, uri, 5*time.Second)
	if err != nil {
		t.Errorf("connect to drop %s: %v", name, err)
		return
	}
	defer func() { _ = client.Disconnect(ctx) }()
	if err := client.Database(name).Drop(ctx); err != nil {
		t.Errorf("drop %s: %v", name, err)
	}
}

type notifyingBuffer struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	written chan struct{}
}

func newNotifyingBuffer() *notifyingBuffer {
	return &notifyingBuffer{written: make(chan struct{}, 1)}
}

func (b *notifyingBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	n, err := b.buf.Write(p)
	b.mu.Unlock()
	select {
	case b.written <- struct{}{}:
	default:
	}
	return n, err
}

func (b *notifyingBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *notifyingBuffer) waitFor(t *testing.T, msg string, done <-chan error) map[string]any {
	t.Helper()

	deadline := time.After(15 * time.Second)
	for {
		for _, line := range strings.Split(b.String(), "\n") {
			var entry map[string]any
			if json.Unmarshal([]byte(line), &entry) == nil && entry["msg"] == msg {
				return entry
			}
		}
		select {
		case <-b.written:
		case err := <-done:
			t.Fatalf("run() returned %v before logging %q; logs:\n%s", err, msg, b.String())
		case <-deadline:
			t.Fatalf("timed out waiting for %q; logs:\n%s", msg, b.String())
		}
	}
}
