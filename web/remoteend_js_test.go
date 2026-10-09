//go:build js && wasm

package web

import (
	"io"
	"testing"

	xterm "github.com/0magnet/xterm-go"
	"github.com/0magnet/xterm-go/vt"

	"github.com/0magnet/websh/shell"
)

// A remote program's requests end where its end shows in the output: it
// leaves the alternate screen, resets the terminal, or the remote shell marks
// its next prompt. This was a real bug, found over a real ssh session: a
// probe exited, the widget it placed stayed, and clicks on it reached the
// remote shell as typed text. dropListen stands for what the program asked
// for: programEnded clears it with the rest.
func TestARemoteProgramsEndIsSeen(t *testing.T) {
	sh, err := shell.New(nil, nil, io.Discard, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	// The core alone: the whole terminal draws, and Node has no DOM to draw in.
	opts := vt.NewOptions()
	opts.Cols, opts.Rows = 80, 24
	s := &Session{Term: &xterm.Terminal{Core: vt.NewTerminal(opts)}, Shell: sh}
	s.wireRemoteEnds()
	ends := func(source, out string) bool {
		t.Helper()
		restore := s.Shell.WithSource(source)
		defer restore()
		s.dropListen = true
		s.Term.Core.WriteString(out)
		return !s.dropListen
	}
	for _, c := range []struct {
		name, source, out string
		want              bool
	}{
		{"remote, alternate screen entered", "remote", "\x1b[?1049h", false},
		{"remote, alternate screen left", "remote", "\x1b[?1049l", true},
		{"remote, reset", "remote", "\x1bc", true},
		{"remote, prompt mark", "remote", "\x1b]133;A\x1b\\", true},
		{"remote, prompt mark with options", "remote", "\x1b]133;A;cl=m\x07", true},
		{"remote, command mark", "remote", "\x1b]133;C\x1b\\", false},
		{"remote, plain output", "remote", "hello\r\n", false},
		{"local, alternate screen left", "local", "\x1b[?1049h\x1b[?1049l", false},
		{"page, reset", "page", "\x1bc", false},
	} {
		if got := ends(c.source, c.out); got != c.want {
			t.Errorf("%s: ended %v, want %v", c.name, got, c.want)
		}
	}
	// And the person's Ctrl+C or Ctrl+\ on its way there, where no mark follows.
	for _, c := range []struct {
		source, typed string
		want          bool
	}{
		{"remote", "\x03", true},
		{"remote", "\x1c", true},
		{"remote", "q", false},
		{"local", "\x03", false},
	} {
		restore := s.Shell.WithSource(c.source)
		s.dropListen = true
		s.interruptTyped(c.typed)
		if got := !s.dropListen; got != c.want {
			t.Errorf("%s, typed %q: ended %v, want %v", c.source, c.typed, got, c.want)
		}
		restore()
	}
	// The terminal still does what the sequences ask: the handlers only look.
	restore := s.Shell.WithSource("remote")
	s.Term.Core.WriteString("\x1b[?1049h")
	if s.Term.Core.Buffers().Active() == s.Term.Core.Buffers().Normal() {
		t.Error("the alternate screen was not entered")
	}
	s.Term.Core.WriteString("\x1bc")
	if s.Term.Core.Buffers().Active() != s.Term.Core.Buffers().Normal() {
		t.Error("a reset did not bring the normal screen back")
	}
	restore()
}
