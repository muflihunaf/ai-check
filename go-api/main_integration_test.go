package main

import (
	"io"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestServerGracefulShutdown(t *testing.T) {
	logger := zap.NewNop()

	requestStarted := make(chan struct{})
	releaseRequest := make(chan struct{})
	defer func() {
		select {
		case <-releaseRequest:
		default:
			close(releaseRequest)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/verify", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-requestStarted:
		default:
			close(requestStarted)
		}
		<-releaseRequest
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	t.Log("creating listener")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	server := &http.Server{Handler: mux}

	signalCh := make(chan os.Signal, 1)
	done := make(chan error, 1)
	go func() {
		done <- serveHTTPServerWithOptions(server, 2*time.Second, logger, listener, signalCh)
	}()

	addr := listener.Addr().String()
	t.Logf("listening on %s", addr)
	waitForServer(t, addr)

	client := &http.Client{Timeout: 2 * time.Second}
	respCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		t.Log("sending request")
		resp, err := client.Get("http://" + addr + "/verify")
		if err != nil {
			errCh <- err
			return
		}
		respCh <- resp
	}()

	select {
	case <-requestStarted:
		t.Log("request started")
	case <-time.After(2 * time.Second):
		t.Fatal("request did not start in time")
	}

	t.Log("sending signal")
	signalCh <- syscall.SIGTERM

	time.Sleep(50 * time.Millisecond)
	close(releaseRequest)
	t.Log("released request")

	select {
	case resp := <-respCh:
		t.Cleanup(func() { resp.Body.Close() })
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("unexpected status: %d body: %s", resp.StatusCode, string(body))
		}
	case err := <-errCh:
		t.Fatalf("request failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("request did not complete")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server did not shutdown cleanly: %v", err)
		}
		t.Log("server shutdown complete")
	case <-time.After(2 * time.Second):
		t.Fatal("server did not exit after shutdown")
	}
}

func waitForServer(t *testing.T, addr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server %s did not become ready", addr)
}
