// Package mosh is websh's mosh client: mosh's UDP datagrams carried over a
// Wisp UDP stream, so that a browser — which cannot send UDP — can reach a
// mosh-server through any Wisp v2 server with the UDP extension.
//
// The protocol is github.com/unixshells/mosh-go; this package supplies the
// transport (Conn), the screen-state tracking that turns mosh's state diffs
// into terminal output, and the shell command.
package mosh

import (
	"errors"
	"os"
	"sync"
	"time"

	"github.com/0magnet/wisp"
)

// Conn presents a Wisp datagram stream as the datagram connection mosh-go's
// client reads and writes (its mosh.Conn): one Write is one datagram, one
// Read returns one, and Read honors SetReadDeadline by failing with an error
// for which os.IsTimeout is true — which is how mosh-go's receive loop tells a
// quiet link from a dead one.
//
// A DatagramStream has no deadline of its own, so a goroutine pumps it into a
// channel and Read waits on that with a timer.
type Conn struct {
	d    wisp.DatagramStream
	in   chan []byte
	done chan struct{}
	once sync.Once

	mu       sync.Mutex
	deadline time.Time
	err      error // why the pump stopped
}

// NewConn wraps a datagram stream. Closing the Conn closes the stream.
func NewConn(d wisp.DatagramStream) *Conn {
	c := &Conn{d: d, in: make(chan []byte, 256), done: make(chan struct{})}
	go c.pump()
	return c
}

func (c *Conn) pump() {
	for {
		b, err := c.d.ReadDatagram()
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			c.Close() //nolint:errcheck,gosec // already failing; the read error is what is reported
			return
		}
		select {
		case c.in <- b:
		case <-c.done:
			return
		default:
			// A full queue drops the datagram, as a full socket buffer
			// would; mosh retransmits what matters.
		}
	}
}

// ErrClosed is returned by Read and Write on a closed Conn.
var ErrClosed = errors.New("mosh: connection closed")

// Read reads one datagram into b, truncating it if b is too small.
func (c *Conn) Read(b []byte) (int, error) {
	c.mu.Lock()
	dl := c.deadline
	c.mu.Unlock()

	var timeout <-chan time.Time
	if !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			// Still hand over a datagram that is already waiting.
			select {
			case p := <-c.in:
				return copy(b, p), nil
			default:
				return 0, os.ErrDeadlineExceeded
			}
		}
		t := time.NewTimer(d)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case p := <-c.in:
		return copy(b, p), nil
	case <-c.done:
		select {
		case p := <-c.in:
			return copy(b, p), nil
		default:
		}
		return 0, c.closeErr()
	case <-timeout:
		return 0, os.ErrDeadlineExceeded
	}
}

func (c *Conn) closeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return ErrClosed
}

// Write sends b as one datagram.
func (c *Conn) Write(b []byte) (int, error) {
	select {
	case <-c.done:
		return 0, c.closeErr()
	default:
	}
	if err := c.d.WriteDatagram(b); err != nil {
		return 0, err
	}
	return len(b), nil
}

// SetReadDeadline sets the deadline for Read; the zero time means none.
func (c *Conn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.deadline = t
	c.mu.Unlock()
	return nil
}

// Done is closed once the connection has closed, from either end.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the connection closed, or nil while it is open.
func (c *Conn) Err() error {
	select {
	case <-c.done:
		return c.closeErr()
	default:
		return nil
	}
}

// Close closes the connection and the stream under it.
func (c *Conn) Close() error {
	var err error
	c.once.Do(func() {
		close(c.done)
		err = c.d.Close()
	})
	return err
}
