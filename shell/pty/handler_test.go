//go:build !js && !windows

package pty

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestHandler(t *testing.T) {
	srv := httptest.NewServer(Handler{Command: func(*http.Request) *exec.Cmd {
		return exec.Command("/bin/sh") //nolint:gosec // a fixed test shell
	}})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	addr, err := WebSocketURL("/pty", srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ws, _, err := websocket.Dial(ctx, addr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.CloseNow() //nolint:errcheck

	if err := ws.Write(ctx, websocket.MessageText, ResizeMessage(100, 30)); err != nil {
		t.Fatal(err)
	}
	if err := ws.Write(ctx, websocket.MessageBinary, []byte("echo HI-$((40+2)); stty size\n")); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	for !strings.Contains(out.String(), "HI-42") || !strings.Contains(out.String(), "30 100") {
		_, b, err := ws.Read(ctx)
		if err != nil {
			t.Fatalf("%v; output so far: %q", err, out.String())
		}
		out.Write(b)
	}
}

func TestHandlerRefusesOtherOrigins(t *testing.T) {
	srv := httptest.NewServer(Handler{Command: func(*http.Request) *exec.Cmd {
		return exec.Command("/bin/sh") //nolint:gosec // a fixed test shell
	}})
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	addr, _ := WebSocketURL("/pty", srv.URL, "") //nolint:errcheck
	_, resp, err := websocket.Dial(ctx, addr, &websocket.DialOptions{HTTPHeader: http.Header{"Origin": {"https://evil.example"}}})
	if err == nil {
		t.Fatal("a page on another site reached the shell")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("want 403, got %v", resp)
	}
}
