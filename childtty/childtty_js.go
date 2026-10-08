//go:build js && wasm

// Package childtty is a program's terminal when websh runs it from the
// filesystem: a child process under bottle's proc, with a terminal of its
// own, as a tcell Tty. Size, raw mode and resizes come from the shell, keys
// are read and the screen drawn through the terminal itself, so it works
// whichever toolchain built the program.
//
//	s, err := childtty.NewScreen()
//
// is all a tcell program needs; where the program is not a child with a
// terminal, it falls back to tcell's own screen. A program that writes
// sequences of its own beside tcell's (websh placements, say) writes them to
// the Tty Open returns, so they land in order: TinyGo holds os.Stdout back
// until a newline. A program TinyGo built must also end with os.Exit, since
// TinyGo keeps a js program alive after main returns.
package childtty

import (
	"sync"

	"github.com/0magnet/bottle/proc"
	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/tty"
)

// Open returns this program's terminal, or false when it has none.
func Open() (tty.Tty, bool) {
	t, ok := proc.Term()
	if !ok {
		return nil, false
	}
	c := &childTty{t: t}
	t.OnResize(c.resized)
	return c, true
}

// NewScreen is a tcell screen on this program's terminal, or tcell's own
// screen where it has none.
func NewScreen() (tcell.Screen, error) {
	if t, ok := Open(); ok {
		return tcell.NewTerminfoScreenFromTty(t)
	}
	return tcell.NewScreen()
}

type childTty struct {
	t  *proc.Terminal
	mu sync.Mutex
	ch chan<- bool
}

func (c *childTty) Start() error { c.t.SetRaw(true); return nil }
func (c *childTty) Stop() error  { c.t.SetRaw(false); return nil }

// Drain does nothing: tcell reads on a goroutine it leaves behind when it
// stops, so a read waiting here does not hold it up.
func (c *childTty) Drain() error { return nil }

func (c *childTty) NotifyResize(ch chan<- bool) {
	c.mu.Lock()
	c.ch = ch
	c.mu.Unlock()
}

func (c *childTty) resized(int, int) {
	c.mu.Lock()
	ch := c.ch
	c.mu.Unlock()
	if ch != nil {
		select {
		case ch <- true:
		default:
		}
	}
}

func (c *childTty) WindowSize() (tty.WindowSize, error) {
	w, h := c.t.Size()
	return tty.WindowSize{Width: w, Height: h}, nil
}

func (c *childTty) Read(p []byte) (int, error)  { return c.t.Read(p) }
func (c *childTty) Write(p []byte) (int, error) { return c.t.Write(p) }
func (c *childTty) Close() error                { return nil }
