//go:build js && wasm

package pty

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
)

// Register adds the pty command to the shell.
func Register() {
	shell.RegisterApplet("pty", "terminal on another machine over a WebSocket (pty --help)", run)
}

// resizePoll is how often the terminal's size is checked; the shell offers
// no event for it.
const resizePoll = 250 * time.Millisecond

// closeNormal is the WebSocket close code a server sends when it ends the
// session itself, as when the shell exits. A dropped connection closes with
// another code, 1006 when it simply broke, and is reconnected.
const closeNormal = 1000

func run(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
	if len(args) != 1 || args[0] == "-h" || args[0] == "--help" {
		shell.Print(hc.Stdout, usage)
		if len(args) == 1 {
			return 0
		}
		return 2
	}
	target := trimArg(args[0])
	base := js.Global().Get("document").Get("baseURI").String()
	if _, err := WebSocketURL(target, base, ""); err != nil {
		shell.Printf(hc.Stderr, "pty: %v\n", err)
		return 2
	}

	defer s.WithSource("remote")() // what arrives is another machine's
	if s.RawMode != nil {
		s.RawMode(true)
		defer s.RawMode(false)
	}

	// The key reader is left blocked on the terminal when the session ends;
	// waking it stops it taking the next key from the shell.
	keys := make(chan []byte, 64)
	stop := make(chan struct{})
	go readKeys(hc.Stdin, keys, stop)
	defer func() {
		close(stop)
		if s.WakeStdin != nil {
			s.WakeStdin()
		}
	}()

	var sid string
	for attempt := 0; ; attempt++ {
		addr, _ := WebSocketURL(target, base, sid) //nolint:errcheck // checked above
		res := connect(ctx, s, hc, addr, keys)
		if res.sid != "" {
			sid = res.sid
		}
		switch {
		case res.quit:
			shell.Print(hc.Stderr, "\r\n[pty: closed]\r\n")
			return 0
		case ctx.Err() != nil:
			return 130
		case res.opened:
			attempt = 0
		}
		if sid == "" || res.ended {
			if res.err != nil {
				shell.Printf(hc.Stderr, "\r\npty: %v\r\n", res.err)
				return 1
			}
			shell.Print(hc.Stderr, "\r\n[pty: session ended]\r\n")
			return 0
		}
		if attempt == 0 {
			shell.Print(hc.Stderr, "\r\n[pty: reconnecting]\r\n")
		}
		select {
		case <-time.After(retryWait(attempt)):
		case <-ctx.Done():
			return 130
		}
	}
}

// readKeys feeds the terminal's input to whichever connection is open.
func readKeys(in io.Reader, keys chan<- []byte, stop <-chan struct{}) {
	buf := make([]byte, 4096)
	for {
		n, err := in.Read(buf)
		if n > 0 {
			select {
			case keys <- append([]byte(nil), buf[:n]...):
			case <-stop:
				return
			}
		}
		if err != nil {
			return
		}
		select {
		case <-stop:
			return
		default:
		}
	}
}

// result is how one connection ended.
type result struct {
	opened bool   // it got as far as open
	quit   bool   // the person typed ~.
	ended  bool   // the server closed normally: the session is over
	sid    string // the session id the server gave, if any
	err    error
}

// event is what a browser callback hands to the connection's loop.
type event struct {
	kind string // open, data, text, close, error
	data []byte
	code int // a close event's code
}

// queue carries events out of the browser's callbacks. It is unbounded and
// its wake-up never blocks, so a burst of output is neither lost nor able to
// stall the event loop.
type queue struct {
	mu   sync.Mutex
	evs  []event
	wake chan struct{}
}

func (q *queue) post(e event) {
	q.mu.Lock()
	q.evs = append(q.evs, e)
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) take() []event {
	q.mu.Lock()
	defer q.mu.Unlock()
	evs := q.evs
	q.evs = nil
	return evs
}

// connect runs one WebSocket until it closes.
func connect(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, addr string, keys <-chan []byte) result {
	q := &queue{wake: make(chan struct{}, 1)}
	ws := js.Global().Get("WebSocket").New(addr)
	ws.Set("binaryType", "arraybuffer")
	onOpen := js.FuncOf(func(js.Value, []js.Value) any { q.post(event{kind: "open"}); return nil })
	onMessage := js.FuncOf(func(_ js.Value, a []js.Value) any {
		d := a[0].Get("data")
		if d.Type() == js.TypeString {
			q.post(event{kind: "text", data: []byte(d.String())})
			return nil
		}
		u8 := js.Global().Get("Uint8Array").New(d)
		b := make([]byte, u8.Get("length").Int())
		js.CopyBytesToGo(b, u8)
		q.post(event{kind: "data", data: b})
		return nil
	})
	onClose := js.FuncOf(func(_ js.Value, a []js.Value) any {
		q.post(event{kind: "close", code: a[0].Get("code").Int()})
		return nil
	})
	onError := js.FuncOf(func(js.Value, []js.Value) any { q.post(event{kind: "error"}); return nil })
	defer onOpen.Release()
	defer onMessage.Release()
	defer onClose.Release()
	defer onError.Release()
	ws.Set("onopen", onOpen)
	ws.Set("onmessage", onMessage)
	ws.Set("onclose", onClose)
	ws.Set("onerror", onError)
	defer ws.Call("close")

	send := func(b []byte, text bool) {
		if ws.Get("readyState").Int() != 1 {
			return
		}
		if text {
			ws.Call("send", string(b))
			return
		}
		u8 := js.Global().Get("Uint8Array").New(len(b))
		js.CopyBytesToJS(u8, b)
		ws.Call("send", u8)
	}

	// Keys typed before the socket opens, on a reconnect say, wait for it
	// rather than being dropped.
	var (
		res        result
		esc        escape
		cols, rows int
		pending    [][]byte
	)
	resize := func() {
		if s.Size == nil {
			return
		}
		c, r := s.Size()
		if c > 0 && r > 0 && (c != cols || r != rows) {
			cols, rows = c, r
			send(ResizeMessage(c, r), true)
		}
	}
	tick := time.NewTicker(resizePoll)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return res
		case <-tick.C:
			if res.opened {
				resize()
			}
		case k := <-keys:
			out, quit := esc.filter(k)
			if len(out) > 0 {
				if res.opened {
					send(out, false)
				} else {
					pending = append(pending, out)
				}
			}
			if quit {
				res.quit = true
				return res
			}
		case <-q.wake:
			for _, e := range q.take() {
				switch e.kind {
				case "open":
					res.opened = true
					resize()
					for _, p := range pending {
						send(p, false)
					}
					pending = nil
				case "data":
					shell.Write(hc.Stdout, e.data)
				case "text":
					if c, ok := ParseControl(e.data); ok {
						if c.Type == "session" && c.ID != "" {
							res.sid = c.ID
						}
						continue
					}
					shell.Write(hc.Stdout, e.data)
				case "error":
					if !res.opened {
						res.err = errors.New("could not connect to " + addr)
					}
				case "close":
					res.ended = e.code == closeNormal
					return res
				}
			}
		}
	}
}

// usage is the applet's help.
const usage = `usage: pty <address>

Opens a terminal on another machine over a WebSocket. The address is ws://,
wss://, http(s):// or a path, resolved against this page's address.

  ~.  at the start of a line ends the session
  ~~  sends one ~

The protocol is in PTY.md.
`

// trimArg drops the shell quoting a pasted address might carry.
func trimArg(s string) string { return strings.Trim(strings.TrimSpace(s), `"'`) }
