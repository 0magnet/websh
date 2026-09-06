//go:build js && wasm

package web

import (
	"io"
	"testing"
	"time"
)

// Input has to reach the command in the order it was typed. This was a real
// bug: every keystroke got its own goroutine writing to one pipe, and Go makes
// no promise about which lands first, so typing "exit" into a full-screen
// applet arrived as "xteh". It stayed hidden while the only applets were tcell
// demos, where a cursor key means the same thing whenever it lands — a
// terminal client is the case where the order IS the content.
func TestStdinArrivesInTheOrderItWasTyped(t *testing.T) {
	r, w := io.Pipe()
	s := &Session{stdinW: w, stdinQ: make(chan []byte, 4096)}
	go s.pumpStdin()

	const want = "the quick brown fox jumps over the lazy dog 0123456789"
	// One call per character, which is what onData does — a keystroke at a
	// time, as fast as the events arrive.
	go func() {
		for i := 0; i < len(want); i++ {
			s.writeStdin([]byte{want[i]})
		}
	}()

	got := make([]byte, len(want))
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(r, got)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("read: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out; got %q so far", got)
	}
	if string(got) != want {
		t.Errorf("input arrived as %q, want %q", got, want)
	}
}

// A full queue must drop rather than block. Blocking here would be on the JS
// callback that delivers the keystroke, so it freezes the page — losing a
// character is the better failure, and it takes a machine, not a person, to
// reach it.
func TestStdinQueueDropsRatherThanBlocks(t *testing.T) {
	_, w := io.Pipe() // nothing reads, so the writer would block forever
	s := &Session{stdinW: w, stdinQ: make(chan []byte, 4)}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			s.writeStdin([]byte("x"))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writeStdin blocked; it must drop instead, or a full queue freezes the page")
	}
}

// Writing to a session that never started, or has closed, must not panic —
// onData can fire from a pending event after Close.
func TestStdinWriteAfterCloseIsSafe(t *testing.T) {
	s := &Session{}
	s.writeStdin([]byte("no queue yet")) // must not panic
	_, w := io.Pipe()
	s2 := &Session{stdinW: w, stdinQ: make(chan []byte, 2)}
	s2.stdinQ = nil
	s2.writeStdin([]byte("queue gone")) // must not panic
}
