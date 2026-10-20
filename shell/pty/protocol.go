// Package pty is a terminal on another machine, reached over a WebSocket: the
// pty applet on the browser side, and Handler, a server for it, natively.
//
// The protocol is small enough to state here, and PTY.md says it in full.
// Binary messages carry terminal bytes, keys one way and output the other.
// Text messages carry control as JSON. The client sends
//
//	{"type":"resize","cols":80,"rows":24}
//
// when it opens and whenever its terminal changes size. A server that keeps
// its shell alive across a dropped connection says so with
//
//	{"type":"session","id":"..."}
//
// and a client that reconnects passes the id back as ?sid=, so the same shell
// is reattached rather than a new one started. Any other text message is
// output.
package pty

import (
	"encoding/json"
	"errors"
	"net/url"
)

// Control is a text message of the protocol.
type Control struct {
	Type string `json:"type"`
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
	ID   string `json:"id,omitempty"`
}

// ResizeMessage is the control message for a terminal of cols by rows.
func ResizeMessage(cols, rows int) []byte {
	b, _ := json.Marshal(Control{Type: "resize", Cols: cols, Rows: rows}) //nolint:errcheck // cannot fail
	return b
}

// ParseControl reads a text message as control, or reports it is output.
func ParseControl(b []byte) (Control, bool) {
	var c Control
	if len(b) == 0 || b[0] != '{' || json.Unmarshal(b, &c) != nil || c.Type == "" {
		return Control{}, false
	}
	return c, true
}

// WebSocketURL resolves target against base, the page's address, as a
// WebSocket address: http becomes ws and https wss. A session id, when
// there is one, goes in ?sid= so the server reattaches that shell.
func WebSocketURL(target, base, sid string) (string, error) {
	if target == "" {
		return "", errors.New("no address")
	}
	b, err := url.Parse(base)
	if err != nil {
		return "", err
	}
	t, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	u := b.ResolveReference(t)
	switch u.Scheme {
	case "http", "ws":
		u.Scheme = "ws"
	case "https", "wss":
		u.Scheme = "wss"
	default:
		return "", errors.New("not an http or ws address: " + target)
	}
	u.Fragment = ""
	if sid != "" {
		q := u.Query()
		q.Set("sid", sid)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// escape finds ssh's escapes in the keys going out: at the start of a line,
// ~. ends the session and ~~ sends one ~.
type escape struct {
	midLine bool
	tilde   bool
}

// filter returns the keys to send, and whether ~. asked to end the session.
func (e *escape) filter(in []byte) (out []byte, quit bool) {
	out = make([]byte, 0, len(in))
	for _, c := range in {
		if e.tilde {
			e.tilde = false
			switch c {
			case '.':
				return out, true
			case '~':
				out = append(out, '~')
				e.midLine = true
				continue
			default:
				out = append(out, '~')
			}
		}
		if !e.midLine && c == '~' {
			e.tilde = true
			continue
		}
		out = append(out, c)
		e.midLine = c != '\r' && c != '\n'
	}
	return out, false
}
