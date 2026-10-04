//go:build !tinygo

package mosh

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
)

const usage = `usage: mosh [--wisp URL] [--key KEY] [--port PORT] [user@]host [port]
       mosh [--wisp URL] [user@]host MOSH CONNECT PORT KEY

Connects to a running mosh-server. Its UDP datagrams travel over a Wisp
server with UDP support (skywire cli wisp serve, or any Wisp v2 server with
the UDP extension), which must be able to reach host.

websh has no ssh, so start the server yourself on the remote host:
    mosh-server new
and give mosh the port and key from the "MOSH CONNECT PORT KEY" line it
prints — pasting that line after the host works too.

  --wisp URL   Wisp endpoint, ws:// or wss://   (default $WISP_URL)
  --key KEY    session key                       (default $MOSH_KEY)
  --port PORT  mosh-server's UDP port

Quit with Ctrl-^ then '.'.
`

// Register adds the mosh command to the shell.
func Register() {
	shell.RegisterApplet("mosh", "mobile shell over a Wisp UDP stream (mosh --help)", runMosh)
}

type args struct {
	wisp, key, host string
	port            uint16
}

func parseArgs(argv []string, env func(string) string) (args, error) {
	a := args{wisp: env("WISP_URL"), key: env("MOSH_KEY")}
	var pos []string
	for len(argv) > 0 {
		arg := argv[0]
		argv = argv[1:]
		name, val, hasVal := strings.Cut(strings.TrimLeft(arg, "-"), "=")
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			pos = append(pos, arg)
			continue
		}
		switch name {
		case "h", "help":
			return a, errHelp
		case "wisp", "key", "port", "p", "k", "w":
		default:
			return a, errors.New("unknown option " + arg)
		}
		if !hasVal {
			if len(argv) == 0 {
				return a, errors.New("option " + arg + " needs a value")
			}
			val, argv = argv[0], argv[1:]
		}
		switch name {
		case "wisp", "w":
			a.wisp = val
		case "key", "k":
			a.key = val
		case "port", "p":
			p, err := parsePort(val)
			if err != nil {
				return a, err
			}
			a.port = p
		}
	}
	if len(pos) == 0 {
		return a, errHelp
	}
	a.host = pos[0]
	if i := strings.LastIndexByte(a.host, '@'); i >= 0 {
		a.host = a.host[i+1:] // the user is whoever ran mosh-server
	}
	switch rest := pos[1:]; {
	case len(rest) == 4 && rest[0] == "MOSH" && rest[1] == "CONNECT":
		p, err := parsePort(rest[2])
		if err != nil {
			return a, err
		}
		a.port, a.key = p, rest[3]
	case len(rest) == 1:
		p, err := parsePort(rest[0])
		if err != nil {
			return a, err
		}
		a.port = p
	case len(rest) > 1:
		return a, errors.New("too many arguments")
	}
	switch {
	case a.wisp == "":
		return a, errors.New("no Wisp server: give --wisp ws://HOST/PATH or set WISP_URL")
	case a.port == 0:
		return a, errors.New("no port: give --port, from mosh-server's MOSH CONNECT line")
	case a.key == "":
		return a, errors.New("no key: give --key or set MOSH_KEY, from mosh-server's MOSH CONNECT line")
	}
	return a, nil
}

var errHelp = errors.New("help")

func parsePort(s string) (uint16, error) {
	p, err := strconv.ParseUint(s, 10, 16)
	if err != nil || p == 0 {
		return 0, errors.New("bad port " + strconv.Quote(s))
	}
	return uint16(p), nil
}

func runMosh(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, argv []string) int {
	a, err := parseArgs(argv, func(name string) string { return hc.Env.Get(name).String() })
	if errors.Is(err, errHelp) {
		shell.Print(hc.Stdout, usage)
		return 0
	}
	if err != nil {
		shell.Printf(hc.Stderr, "mosh: %v\n", err)
		return 1
	}
	if s.RawMode != nil {
		s.RawMode(true)
		defer s.RawMode(false)
	}
	err = Run(ctx, Config{
		WispURL: a.wisp,
		Host:    a.host,
		Port:    a.port,
		Key:     a.key,
		Stdin:   hc.Stdin,
		Stdout:  hc.Stdout,
		Size:    s.Size,
	})
	if err != nil {
		shell.Printf(hc.Stderr, "mosh: %v\n", err)
		return 1
	}
	return 0
}
