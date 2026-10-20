//go:build !js && !windows

package pty

import (
	"context"
	"net/http"
	"os/exec"
	"sync"

	"github.com/coder/websocket"
	creack "github.com/creack/pty"
)

// Handler serves the protocol over a local pseudo-terminal: each connection
// runs the command Command makes in a terminal of its own, and ends it when
// the connection closes. It keeps no session across a drop, so it never sends
// a session id.
//
// A page on another site must not reach a shell, so a connection whose Origin
// is not the server's own is refused; set OriginPatterns to allow others.
type Handler struct {
	Command        func(r *http.Request) *exec.Cmd
	OriginPatterns []string
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.OriginPatterns})
	if err != nil {
		return
	}
	defer ws.CloseNow() //nolint:errcheck // the session is over either way
	ws.SetReadLimit(1 << 20)

	cmd := h.Command(r)
	f, err := creack.StartWithSize(cmd, &creack.Winsize{Cols: 80, Rows: 24})
	if err != nil {
		ws.Close(websocket.StatusInternalError, err.Error()) //nolint:errcheck,gosec // nowhere to report it
		return
	}
	defer func() {
		f.Close() //nolint:errcheck,gosec // the session is over either way
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck,gosec // it may have exited already
		}
		cmd.Wait() //nolint:errcheck,gosec // its status has nowhere to go
	}()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var once sync.Once
	end := func() { once.Do(cancel) }

	go func() {
		defer end()
		buf := make([]byte, 32*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 && ws.Write(ctx, websocket.MessageBinary, buf[:n]) != nil {
				return
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer end()
		for {
			typ, b, err := ws.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageText {
				if c, ok := ParseControl(b); ok {
					if c.Type == "resize" && c.Cols > 0 && c.Rows > 0 && c.Cols <= 0xffff && c.Rows <= 0xffff {
						creack.Setsize(f, &creack.Winsize{Cols: uint16(c.Cols), Rows: uint16(c.Rows)}) //nolint:errcheck,gosec // bounds checked; a failed resize changes nothing
					}
					continue
				}
			}
			if _, err := f.Write(b); err != nil {
				return
			}
		}
	}()
	<-ctx.Done()
	ws.Close(websocket.StatusNormalClosure, "session ended") //nolint:errcheck,gosec // nowhere to report it
}
