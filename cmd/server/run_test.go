package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	t.Run("nil_handler", func(t *testing.T) {
		if err := run(context.Background(), nil, "127.0.0.1:0"); err == nil {
			t.Fatal("expected nil handler to fail")
		}
	})
	t.Run("already_canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := run(ctx, http.NotFoundHandler(), "127.0.0.1:0"); err != nil {
			t.Fatalf("canceled before startup: %v", err)
		}
	})
	t.Run("invalid_address", func(t *testing.T) {
		err := run(context.Background(), http.NotFoundHandler(), "not-an-address")
		var addrErr *net.AddrError
		if !errors.As(err, &addrErr) {
			t.Fatalf("expected preserved address error, got %v", err)
		}
	})
	t.Run("occupied_port", func(t *testing.T) {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err = run(ctx, http.NotFoundHandler(), listener.Addr().String())
		var opErr *net.OpError
		if !errors.As(err, &opErr) {
			t.Fatalf("expected preserved listen error, got %v", err)
		}
	})
	t.Run("serve_cancel_and_release_port", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, "ok")
		})
		addr, cancel, done := startRun(t, handler)
		client := newTestClient(t)
		resp, err := client.Get("http://" + addr + "/healthz")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || resp.StatusCode != 200 || string(body) != "ok" {
			t.Fatalf("unexpected response: %d %q %v", resp.StatusCode, body, err)
		}
		cancel()
		if err := awaitRun(t, done, 2*time.Second); err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("port was not released: %v", err)
		}
		listener.Close()
	})
	t.Run("drain_active_request", func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		var once sync.Once
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			close(entered)
			select {
			case <-release:
				_, _ = io.WriteString(w, "finished")
			case <-req.Context().Done():
			}
		})
		addr, cancel, done := startRun(t, handler)
		t.Cleanup(func() { once.Do(func() { close(release) }) })
		client := newTestClient(t)
		response := make(chan error, 1)
		go func() {
			resp, err := client.Get("http://" + addr + "/work")
			if err == nil {
				body, readErr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if readErr != nil {
					err = readErr
				} else if string(body) != "finished" {
					err = fmt.Errorf("unexpected body %q", body)
				}
			}
			response <- err
		}()
		waitSignal(t, entered, "handler start")
		cancel()
		select {
		case err := <-done:
			t.Fatalf("run returned before active request finished: %v", err)
		case <-time.After(100 * time.Millisecond):
		}
		once.Do(func() { close(release) })
		select {
		case err := <-response:
			if err != nil {
				t.Fatalf("active request interrupted: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("active response did not finish")
		}
		if err := awaitRun(t, done, 2*time.Second); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("shutdown_deadline_force_closes_connection", func(t *testing.T) {
		entered := make(chan struct{})
		exited := make(chan struct{})
		handler := http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
			close(entered)
			<-req.Context().Done()
			close(exited)
		})
		addr, cancel, done := startRun(t, handler)
		client := newTestClient(t)
		client.Timeout = 12 * time.Second
		response := make(chan error, 1)
		go func() {
			resp, err := client.Get("http://" + addr + "/slow")
			if resp != nil {
				resp.Body.Close()
			}
			response <- err
		}()
		waitSignal(t, entered, "handler start")
		cancel()
		if err := awaitRun(t, done, 9*time.Second); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected preserved shutdown deadline, got %v", err)
		}
		waitSignal(t, exited, "request context cancellation")
		select {
		case err := <-response:
			if err == nil {
				t.Fatal("expected forcibly closed request to fail")
			}
		case <-time.After(2 * time.Second):
			t.Fatal("client request was not released")
		}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("port still held after forced close: %v", err)
		}
		listener.Close()
	})
}

func startRun(t *testing.T, handler http.Handler) (string, context.CancelFunc, <-chan error) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, handler, addr)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(9 * time.Second):
			t.Error("run goroutine did not finish during cleanup")
		}
	})
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("run ended before listening: %v", err)
		default:
		}
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return addr, cancel, done
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("server did not start listening")
	return "", cancel, done
}

func newTestClient(t *testing.T) *http.Client {
	t.Helper()
	transport := &http.Transport{DisableKeepAlives: true}
	t.Cleanup(transport.CloseIdleConnections)
	return &http.Client{Transport: transport, Timeout: 2 * time.Second}
}

func awaitRun(t *testing.T, done <-chan error, timeout time.Duration) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(timeout):
		t.Fatal("run did not finish within timeout")
		return nil
	}
}

func waitSignal(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestStartRejectsInvalidConfig(t *testing.T) {
	t.Setenv("ASTERFLOW_HTTP_ADDR", "")
	if err := start(); err == nil || !strings.Contains(err.Error(), "ASTERFLOW_HTTP_ADDR") {
		t.Fatalf("expected startup configuration error, got %v", err)
	}
}

func TestMainProcess(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "asterflow")
	buildCtx, cancelBuild := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancelBuild()
	build := exec.CommandContext(buildCtx, "go", "build", "-mod=readonly", "-o", binary, ".")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, output)
	}

	t.Run("invalid_config_exit_one", func(t *testing.T) {
		assertStartupExitOne(t, binary, "", "ASTERFLOW_HTTP_ADDR")
	})

	t.Run("missing_database_config_exit_one", func(t *testing.T) {
		t.Setenv("ASTERFLOW_TEST_DATABASE_URL", "")
		assertStartupExitOne(t, binary, "127.0.0.1:18090", "ASTERFLOW_DATABASE_URL")
	})
	t.Run("invalid_database_config_exit_one", func(t *testing.T) {
		const secret = "test-password-must-not-be-logged"
		t.Setenv("ASTERFLOW_TEST_DATABASE_URL",
			"postgres://unit:"+secret+"@127.0.0.1:invalid/unit")
		output := assertStartupExitOne(t, binary, "127.0.0.1:18090", "open PostgreSQL")
		if strings.Contains(output, secret) || strings.Contains(output, "starting HTTP server") {
			t.Fatal("invalid database configuration exposed credentials or started HTTP")
		}
	})
	t.Run("unavailable_database_does_not_bind_http", func(t *testing.T) {
		// Empty private directory cannot contain a PostgreSQL Unix socket.
		t.Setenv("ASTERFLOW_TEST_DATABASE_URL", "host="+t.TempDir()+" dbname=unit user=unit sslmode=disable")
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		addr := probe.Addr().String()
		probe.Close()
		output := assertStartupExitOne(t, binary, addr, "open PostgreSQL")
		if strings.Contains(output, "starting HTTP server") {
			t.Fatal("HTTP startup proceeded after PostgreSQL connection failure")
		}
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			t.Fatalf("failed startup left the HTTP port occupied: %v", err)
		}
		listener.Close()
	})

	t.Run("occupied_port_exit_one", func(t *testing.T) {
		if os.Getenv("ASTERFLOW_TEST_DATABASE_URL") == "" {
			t.Skip("set ASTERFLOW_TEST_DATABASE_URL for real PostgreSQL process verification")
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		assertStartupExitOne(t, binary, listener.Addr().String(), "listen HTTP")
	})
	t.Run("sigint_health_and_exit_zero", func(t *testing.T) {
		if os.Getenv("ASTERFLOW_TEST_DATABASE_URL") == "" {
			t.Skip("set ASTERFLOW_TEST_DATABASE_URL for real PostgreSQL process verification")
		}
		assertSignalShutdown(t, binary, os.Interrupt)
	})
	t.Run("sigterm_health_and_exit_zero", func(t *testing.T) {
		if os.Getenv("ASTERFLOW_TEST_DATABASE_URL") == "" {
			t.Skip("set ASTERFLOW_TEST_DATABASE_URL for real PostgreSQL process verification")
		}
		assertSignalShutdown(t, binary, syscall.SIGTERM)
	})
}

func processEnvironment(addr string) []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, value := range os.Environ() {
		if strings.HasPrefix(value, "ASTERFLOW_HTTP_ADDR=") ||
			strings.HasPrefix(value, "GIN_MODE=") ||
			strings.HasPrefix(value, "ASTERFLOW_DATABASE_URL=") {
			continue
		}
		env = append(env, value)
	}
	// Subprocesses may only use the explicitly selected test database.
	databaseURL := os.Getenv("ASTERFLOW_TEST_DATABASE_URL")
	return append(env, "ASTERFLOW_HTTP_ADDR="+addr, "GIN_MODE=test",
		"ASTERFLOW_DATABASE_URL="+databaseURL)
}

func assertStartupExitOne(t *testing.T, binary, addr, message string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary)
	command.Env = processEnvironment(addr)
	output, err := command.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("want exit 1, got %v\n%s", err, output)
	}
	if !strings.Contains(string(output), message) {
		t.Fatalf("missing expected error %q in %s", message, output)
	}
	return string(output)
}

func assertSignalShutdown(t *testing.T, binary string, signal os.Signal) {
	t.Helper()
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := probe.Addr().String()
	probe.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	var output bytes.Buffer
	command := exec.CommandContext(ctx, binary)
	command.Env = processEnvironment(addr)
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Start(); err != nil {
		cancel()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- command.Wait()
		close(done)
	}()
	cleanup := func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("subprocess did not finish during cleanup")
		}
	}
	t.Cleanup(cleanup)

	client := newTestClient(t)
	deadline := time.Now().Add(5 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			t.Fatalf("process ended before health check: %v\n%s", err, output.String())
		default:
		}
		resp, err := client.Get("http://" + addr + "/healthz")
		if err == nil {
			body, readErr := io.ReadAll(resp.Body)
			resp.Body.Close()
			if readErr == nil && resp.StatusCode == 200 && string(body) == "ok" {
				ready = true
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !ready {
		cleanup()
		t.Fatalf("health endpoint was not ready\n%s", output.String())
	}
	resp, err := client.Get("http://" + addr + "/readyz")
	if err != nil {
		t.Fatalf("real PostgreSQL readiness failed: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil || resp.StatusCode != http.StatusOK || string(body) != "ok" {
		t.Fatalf("want real readiness 200 ok, got %d %q %v", resp.StatusCode, body, readErr)
	}
	if err := command.Process.Signal(signal); err != nil {
		t.Fatalf("send %v: %v", signal, err)
	}
	if err := awaitRun(t, done, 3*time.Second); err != nil {
		t.Fatalf("signal %v should exit 0: %v\n%s", signal, err, output.String())
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port not released after %v: %v", signal, err)
	}
	listener.Close()
}
