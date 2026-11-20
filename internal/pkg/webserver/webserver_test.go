package webserver

import (
	"bufio"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// serve runs newHTTPServer on a free port with the timeouts of the test, returns its address
func serve(t *testing.T) string {
	t.Helper()
	saved := []time.Duration{ReadHeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout}
	t.Cleanup(func() {
		ReadHeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout = saved[0], saved[1], saved[2], saved[3]
	})
	ReadHeaderTimeout, ReadTimeout, WriteTimeout, IdleTimeout = 200*time.Millisecond, time.Second, time.Second, time.Second
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := newHTTPServer(listener.Addr().String(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close() })
	return listener.Addr().String()
}

func TestSlowHeadersClosed(t *testing.T) {
	addr := serve(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// a request whose headers never end
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\n"); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	start := time.Now()
	response, err := io.ReadAll(conn)
	if err != nil {
		t.Fatalf("the connection is still open after %v: %v", time.Since(start), err)
	}
	// the server may answer 408 before closing; never the handler's "ok"
	if strings.Contains(string(response), "ok") && !strings.Contains(string(response), "408") {
		t.Errorf("the handler ran on incomplete headers: %q", response)
	}
}

func TestCompleteRequestServed(t *testing.T) {
	addr := serve(t)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || string(body) != "ok" {
		t.Errorf("%d %q, want 200 ok", response.StatusCode, body)
	}
}

func TestServerLimits(t *testing.T) {
	server := newHTTPServer(":0", http.NotFoundHandler())
	if server.ReadHeaderTimeout <= 0 || server.ReadTimeout <= 0 || server.WriteTimeout <= 0 || server.IdleTimeout <= 0 {
		t.Errorf("a connection without limit: %+v", server)
	}
	if server.WriteTimeout < server.ReadTimeout {
		t.Error("WriteTimeout below ReadTimeout: an upload read slowly but in time gets no response")
	}
	if server.MaxHeaderBytes != MaxHeaderBytes {
		t.Errorf("MaxHeaderBytes %d", server.MaxHeaderBytes)
	}
}
