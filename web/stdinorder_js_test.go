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
	s := &Session{in: newInQueue()}
	r := s.in

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
	s := &Session{in: newInQueue()} // nothing reads it

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10000; i++ {
			s.writeStdin(make([]byte, 1024)) // ten times what it holds
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
	s2 := &Session{in: newInQueue()}
	s2.in.close()
	s2.writeStdin([]byte("queue gone")) // must not panic
	if n, err := s2.in.Read(make([]byte, 8)); n != 0 || err != io.EOF {
		t.Errorf("closed queue read %d, %v", n, err)
	}
}

// A reply the terminal gives a command's query is that command's: one it
// never read (printf '\e[c') must not reach the next command that reads
// stdin, as though typed. Typed keys carry over, as type-ahead does.
func TestUnreadRepliesDoNotOutliveTheirCommand(t *testing.T) {
	q := newInQueue()
	q.next() // command 1 asks and never reads
	q.push(inItem{b: []byte("\x1b[?1;2c"), reply: true})
	q.push(inItem{b: []byte("typed ahead ")})
	q.next()                                            // command 2 reads
	q.push(inItem{b: []byte("\x1b[1;1R"), reply: true}) // its own query's answer
	got := make([]byte, 64)
	n1, err1 := q.Read(got)
	n2, err2 := q.Read(got[n1:])
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	if s := string(got[:n1+n2]); s != "typed ahead \x1b[1;1R" {
		t.Errorf("command 2 read %q", s)
	}
	q.push(inItem{wake: true})
	if n, err := q.Read(got); n != 0 || err != nil {
		t.Errorf("wake read %d, %v", n, err)
	}
}
