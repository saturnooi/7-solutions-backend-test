package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestServeGRPC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(*grpc.Server, net.Listener)
		wantErr error
	}{
		{
			name:    "a server stopped by shutdown is a clean exit",
			prepare: func(server *grpc.Server, _ net.Listener) { server.Stop() },
		},
		{
			name:    "a broken listener is a failure",
			prepare: func(_ *grpc.Server, listener net.Listener) { _ = listener.Close() },
			wantErr: net.ErrClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			listener := listen(t)
			server := grpc.NewServer()
			t.Cleanup(server.Stop)
			tt.prepare(server, listener)

			err := serveGRPC(server, listener)

			if tt.wantErr == nil && err != nil {
				t.Errorf("serveGRPC() error = %v, want nil", err)
			}
			if tt.wantErr != nil && (!errors.Is(err, tt.wantErr) || !strings.HasPrefix(err.Error(), "grpc server: ")) {
				t.Errorf("serveGRPC() error = %v, want a \"grpc server: \" error wrapping %v", err, tt.wantErr)
			}
			if closeErr := listener.Close(); !errors.Is(closeErr, net.ErrClosed) {
				t.Errorf("listener still open after serveGRPC() returned (Close() = %v), want it released", closeErr)
			}
		})
	}
}

func TestShutdown_StopsGRPCWhileHTTPDrains(t *testing.T) {
	t.Parallel()

	entered := make(chan struct{})
	release := make(chan struct{})
	releaseHandler := sync.OnceFunc(func() { close(release) })
	httpServer := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			close(entered)
			<-release
			w.WriteHeader(http.StatusNoContent)
		}),
	}
	httpListener := listen(t)
	grpcListener := listen(t)
	grpcServer := grpc.NewServer()
	t.Cleanup(func() { _ = httpServer.Close() })
	t.Cleanup(grpcServer.Stop)
	t.Cleanup(releaseHandler)
	go func() { _ = httpServer.Serve(httpListener) }()
	grpcDone := make(chan error, 1)
	go func() { grpcDone <- serveGRPC(grpcServer, grpcListener) }()

	requestDone := make(chan int, 1)
	go func() {
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+httpListener.Addr().String(), nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			requestDone <- 0
			return
		}
		_ = resp.Body.Close()
		requestDone <- resp.StatusCode
	}()
	receive(t, entered, "the request never reached the handler")

	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- shutdown(t.Context(), slog.New(slog.DiscardHandler), httpServer, grpcServer)
	}()

	if err := receive(t, grpcDone, "gRPC server did not stop while an HTTP request was still in flight"); err != nil {
		t.Fatalf("serveGRPC() error = %v, want nil", err)
	}
	select {
	case err := <-shutdownDone:
		t.Fatalf("shutdown() returned %v before the in-flight HTTP request finished", err)
	default:
	}

	releaseHandler()
	if err := receive(t, shutdownDone, "shutdown() did not return after the HTTP request finished"); err != nil {
		t.Errorf("shutdown() error = %v, want nil", err)
	}
	if status := receive(t, requestDone, "the in-flight request never completed"); status != http.StatusNoContent {
		t.Errorf("in-flight request status = %d, want %d", status, http.StatusNoContent)
	}
}

func TestShutdown_CutsOffBothServersWhenTheDeadlinePasses(t *testing.T) {
	t.Parallel()

	httpEntered := make(chan struct{})
	httpCanceled := make(chan struct{})
	httpServer := &http.Server{
		ReadHeaderTimeout: time.Second,
		Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(httpEntered)
			<-r.Context().Done()
			close(httpCanceled)
		}),
	}
	httpListener := listen(t)
	t.Cleanup(func() { _ = httpServer.Close() })
	go func() { _ = httpServer.Serve(httpListener) }()
	go func() {
		req, _ := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+httpListener.Addr().String(), nil)
		if resp, err := http.DefaultClient.Do(req); err == nil {
			_ = resp.Body.Close()
		}
	}()

	rpcEntered := make(chan struct{})
	rpcCanceled := make(chan struct{})
	grpcServer := grpc.NewServer(grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		close(rpcEntered)
		<-stream.Context().Done()
		close(rpcCanceled)
		return stream.Context().Err()
	}))
	grpcListener := listen(t)
	t.Cleanup(grpcServer.Stop)
	go func() { _ = serveGRPC(grpcServer, grpcListener) }()
	conn, err := grpc.NewClient(grpcListener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("grpc client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() { _ = conn.Invoke(t.Context(), "/test.Blocking/Wait", &emptypb.Empty{}, &emptypb.Empty{}) }()
	receive(t, httpEntered, "the HTTP request never reached the handler")
	receive(t, rpcEntered, "the RPC never reached the server")

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- shutdown(ctx, slog.New(slog.DiscardHandler), httpServer, grpcServer)
	}()
	err = receive(t, shutdownDone, "shutdown() did not return after its deadline")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("shutdown() error = %v, want it to wrap %v", err, context.DeadlineExceeded)
	}
	for _, cause := range []string{"http server shutdown: ", "grpc server shutdown: "} {
		if err == nil || !strings.Contains(err.Error(), cause) {
			t.Errorf("shutdown() error = %v, want it to report %q", err, cause)
		}
	}
	receive(t, httpCanceled, "the in-flight HTTP handler kept running after the shutdown deadline")
	receive(t, rpcCanceled, "the in-flight RPC kept running after the shutdown deadline")
}

func listen(t *testing.T) net.Listener {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	return listener
}

func receive[T any](t *testing.T, ch <-chan T, failure string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal(failure)
		var zero T
		return zero
	}
}
