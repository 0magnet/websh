//go:build js && wasm

package web

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// recorder is a terminal that decodes each write on its own, as xterm-go does:
// a write that is not whole characters is what it draws wrong.
type recorder struct {
	b       strings.Builder
	invalid bool
}

func (r *recorder) WriteString(s string) {
	if !utf8.ValidString(s) {
		r.invalid = true
	}
	r.b.WriteString(s)
}

// A pty is read in pieces, and a piece may end inside a character. This was a
// real bug, seen with a desktop host's pty: the halves of a ▀ were drawn as
// replacement characters, three cells for one, and pushed the rest of the row
// past where the program drew it.
func TestTermWriterKeepsSplitCharacters(t *testing.T) {
	for _, s := range []string{"▀▀▀ half blocks ▀", "an emoji 🧲 and 漢字", "plain"} {
		for cut := 0; cut <= len(s); cut++ {
			r := &recorder{}
			w := &termWriter{term: r, pty: func() bool { return true }}
			w.Write([]byte(s[:cut])) //nolint:errcheck,gosec // into memory
			w.Write([]byte(s[cut:])) //nolint:errcheck,gosec // into memory
			if got := r.b.String(); got != s || r.invalid {
				t.Fatalf("cut at %d: drew %q (a write split a character: %v), want %q", cut, got, r.invalid, s)
			}
		}
	}
}

// A pty's line feeds are its own; what the shell writes cooked gets the
// carriage return a terminal's output processing adds.
func TestTermWriterLineFeeds(t *testing.T) {
	for _, c := range []struct {
		pty      bool
		in, want string
	}{
		{true, "a\nb\r\n", "a\nb\r\n"},
		{false, "a\nb\n", "a\r\nb\r\n"},
	} {
		r := &recorder{}
		w := &termWriter{term: r, pty: func() bool { return c.pty }}
		w.Write([]byte(c.in)) //nolint:errcheck,gosec // into memory
		if got := r.b.String(); got != c.want {
			t.Errorf("pty %v: drew %q, want %q", c.pty, got, c.want)
		}
	}
}
