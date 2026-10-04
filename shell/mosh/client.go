//go:build !tinygo

package mosh

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/0magnet/wisp"
	moshgo "github.com/unixshells/mosh-go"
)

// Config is one mosh session.
type Config struct {
	// WispURL is the ws:// or wss:// endpoint of a Wisp server with UDP.
	WispURL string
	// Host and Port are the mosh-server's UDP address, as the Wisp server
	// sees it.
	Host string
	Port uint16
	// Key is the session key from the server's MOSH CONNECT line.
	Key string

	// Stdin carries the keys typed, raw. Stdout is the terminal.
	Stdin  io.Reader
	Stdout io.Writer
	// Size reports the terminal size; it is polled, so a resize reaches the
	// server within a tick.
	Size func() (cols, rows int)
}

// tick is how often pending keystrokes and acknowledgments are flushed and
// the screen is redrawn — mosh's own minimum send interval.
const tick = 8 * time.Millisecond

// escape is mosh's escape key, Ctrl-^; it followed by . quits.
const escape = 0x1e

// Dial opens the datagram path to a mosh-server: a Wisp session, then a UDP
// stream through it.
func Dial(ctx context.Context, wispURL, host string, port uint16) (*wisp.Client, *Conn, error) {
	wc, err := wisp.Dial(ctx, wisp.ClientConfig{URL: wispURL})
	if err != nil {
		return nil, nil, fmt.Errorf("wisp: %w", err)
	}
	if wc.Version() >= 2 && !wc.UDPSupported() {
		wc.Close() //nolint:errcheck,gosec // reporting the real problem
		return nil, nil, errors.New("the Wisp server does not offer UDP streams")
	}
	d, err := wc.DialUDP(ctx, host, port)
	if err != nil {
		wc.Close() //nolint:errcheck,gosec // reporting the real problem
		return nil, nil, err
	}
	return wc, NewConn(d), nil
}

// DecodeKey decodes a mosh session key: 22 characters of base64, unpadded.
func DecodeKey(key string) ([]byte, error) {
	key = strings.TrimSpace(key)
	for len(key)%4 != 0 {
		key += "="
	}
	raw, err := base64.StdEncoding.DecodeString(key)
	if err != nil || len(raw) != 16 {
		return nil, errors.New("bad key: want the 22-character key from MOSH CONNECT")
	}
	return raw, nil
}

// Run runs a session until the user quits with Ctrl-^ . or ctx ends. Stdin is
// read on the calling goroutine, and Run returns only after a read returns, so
// no reader is left behind to swallow the next command's input.
func Run(ctx context.Context, cfg Config) error {
	raw, err := DecodeKey(cfg.Key)
	if err != nil {
		return err
	}
	ocb, err := moshgo.NewOCB(raw)
	if err != nil {
		return err
	}
	wc, conn, err := Dial(ctx, cfg.WispURL, cfg.Host, cfg.Port)
	if err != nil {
		return err
	}
	defer wc.Close() //nolint:errcheck // the session is over either way

	client, err := moshgo.DialConnRaw(conn, ocb)
	if err != nil {
		conn.Close() //nolint:errcheck,gosec // reporting the real problem
		return err
	}

	cols, rows := 80, 24
	if cfg.Size != nil {
		cols, rows = cfg.Size()
	}
	client.Resize(uint16(cols), uint16(rows)) //nolint:gosec // terminal sizes are small
	scr := newScreen(cols, rows)

	var outMu sync.Mutex
	write := func(b []byte) {
		outMu.Lock()
		defer outMu.Unlock()
		cfg.Stdout.Write(b) //nolint:errcheck,gosec // nowhere to report a terminal write error
	}
	// The alternate screen, so the shell's scrollback comes back afterwards.
	write([]byte(fmt.Sprintf("\x1b[?1049h\x1b[H\x1b[2Jmosh: connecting to %s:%d ...\r\n", cfg.Host, cfg.Port)))

	done := make(chan struct{})
	var stopOnce sync.Once
	var why error
	stop := func(err error) {
		stopOnce.Do(func() {
			why = err
			close(done)
			if err != nil && !errors.Is(err, context.Canceled) {
				// The key reader is still waiting for a key.
				write([]byte("\r\n\x1b[7m mosh: " + err.Error() + " — press any key \x1b[0m"))
			}
		})
	}
	var wg sync.WaitGroup
	wg.Add(2)

	// receive: diffs into the screen model
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
			}
			diff := client.RecvRaw(250 * time.Millisecond)
			if diff == nil {
				if err := conn.Err(); err != nil {
					stop(fmt.Errorf("connection lost: %w", err))
					return
				}
				continue
			}
			t := client.Transport()
			scr.apply(diff, t.LastRecvOldNum(), t.LastRecvNewNum(), t.ThrowawayNum())
		}
	}()

	// tick: flush input, follow the size, draw
	go func() {
		defer wg.Done()
		tk := time.NewTicker(tick)
		defer tk.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				stop(ctx.Err())
				return
			case <-tk.C:
			}
			if cfg.Size != nil {
				if c, r := cfg.Size(); c != cols || r != rows {
					cols, rows = c, r
					client.Resize(uint16(c), uint16(r)) //nolint:gosec // terminal sizes are small
					scr.resize(c, r)
				}
			}
			client.Tick()
			if out := scr.take(); len(out) > 0 {
				write(out)
			}
		}
	}()

	readKeys(cfg.Stdin, client, done, stop)

	stop(nil)
	wg.Wait()
	client.Close()
	write([]byte("\x1b[0m\x1b[?25h\x1b[?1049l"))
	if why != nil && !errors.Is(why, context.Canceled) {
		return why
	}
	return nil
}

// readKeys forwards keystrokes until Ctrl-^ ., end of input, or — once the
// session has ended some other way — the next key, which is then not sent.
func readKeys(r io.Reader, client *moshgo.Client, done <-chan struct{}, stop func(error)) {
	buf := make([]byte, 4096)
	pendingEsc := false
	for {
		n, err := r.Read(buf)
		select {
		case <-done:
			return
		default:
		}
		if n > 0 {
			var send []byte
			for _, b := range buf[:n] {
				switch {
				case pendingEsc:
					pendingEsc = false
					switch b {
					case '.':
						if len(send) > 0 {
							client.Send(send)
						}
						return
					case escape:
						send = append(send, escape)
					default:
						send = append(send, escape, b)
					}
				case b == escape:
					pendingEsc = true
				default:
					send = append(send, b)
				}
			}
			if len(send) > 0 {
				client.Send(send)
			}
		}
		if err != nil {
			stop(nil)
			return
		}
	}
}
