//go:build !js && !tinygo

package mosh

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	moshgo "github.com/unixshells/mosh-go"
)

// lockedBuffer is the terminal: Run writes it from its tick goroutine.
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// pipeRW is the server's side of a session in place of a pty.
type pipeRW struct {
	io.Reader
	io.Writer
}

func (pipeRW) Close() error { return nil }

// TestSessionEndToEnd runs a whole session: keys typed into Run go through
// the Wisp server to mosh-go's server, which plays the remote program here —
// it answers every key with a line — and the screen state comes back.
func TestSessionEndToEnd(t *testing.T) {
	url := wispServer(t)

	srv, err := moshgo.NewServer("", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	keysR, keysW := io.Pipe()                                // what the server's "program" reads
	outR, outW := io.Pipe()                                  // what it writes
	go srv.ServeRW(pipeRW{Reader: outR, Writer: keysW}, nil) //nolint:errcheck // runs until the test process ends
	// Not srv.Close: after ServeRW it dereferences the pty command ServeRW
	// never started (mosh-go v0.5.2). Ending the program ends the session.
	defer outW.Close()  //nolint:errcheck // test teardown
	defer keysR.Close() //nolint:errcheck // test teardown
	go func() {
		outW.Write([]byte("ready\r\n")) //nolint:errcheck,gosec // test program
		buf := make([]byte, 64)
		for {
			n, err := keysR.Read(buf)
			if err != nil {
				return
			}
			outW.Write([]byte("got " + strings.TrimSpace(string(buf[:n])) + "\r\n")) //nolint:errcheck,gosec // test program
		}
	}()

	stdinR, stdinW := io.Pipe()
	term := &lockedBuffer{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	errc := make(chan error, 1)
	go func() {
		errc <- Run(ctx, Config{
			WispURL: url,
			Host:    "127.0.0.1",
			Port:    uint16(srv.Port()), //nolint:gosec // a port fits
			Key:     srv.KeyBase64(),
			Stdin:   stdinR,
			Stdout:  term,
			Size:    func() (int, int) { return 60, 10 },
		})
	}()

	waitFor := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for !strings.Contains(term.String(), want) {
			if time.Now().After(deadline) {
				t.Fatalf("never saw %q; terminal got %q", want, term.String())
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("ready")
	stdinW.Write([]byte("hello\r")) //nolint:errcheck,gosec // test input
	waitFor("got hello")

	// Ctrl-^ . quits, and Run leaves the alternate screen.
	stdinW.Write([]byte{escape, '.'}) //nolint:errcheck,gosec // test input
	select {
	case err := <-errc:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Ctrl-^ .")
	}
	if !strings.HasSuffix(term.String(), "\x1b[?1049l") {
		t.Errorf("did not leave the alternate screen: %q", term.String()[max(0, len(term.String())-40):])
	}
}

func TestParseArgs(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	a, err := parseArgs([]string{"--wisp", "ws://w/wisp", "--key", "K", "--port", "60001", "me@box"}, env(nil))
	if err != nil || a.host != "box" || a.port != 60001 || a.key != "K" || a.wisp != "ws://w/wisp" {
		t.Fatalf("flags: %+v %v", a, err)
	}
	a, err = parseArgs([]string{"box", "MOSH", "CONNECT", "60002", "KEY2"}, env(map[string]string{"WISP_URL": "ws://e"}))
	if err != nil || a.port != 60002 || a.key != "KEY2" || a.wisp != "ws://e" {
		t.Fatalf("connect line: %+v %v", a, err)
	}
	a, err = parseArgs([]string{"box", "60003"}, env(map[string]string{"WISP_URL": "ws://e", "MOSH_KEY": "K3"}))
	if err != nil || a.port != 60003 || a.key != "K3" {
		t.Fatalf("env: %+v %v", a, err)
	}
	if _, err := parseArgs([]string{"--key", "K", "--port", "1", "box"}, env(nil)); err == nil {
		t.Fatal("no Wisp URL accepted")
	}
	if _, err := parseArgs(nil, env(nil)); err != errHelp {
		t.Fatalf("no args = %v", err)
	}
}
