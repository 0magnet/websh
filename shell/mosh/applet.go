//go:build !tinygo

package mosh

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
	"github.com/0magnet/websh/shell/ssh"
)

const usage = `usage: mosh [--wisp URL] [--ssh-port PORT] [--port PORT] [user@]host
       mosh [--wisp URL] --key KEY --port PORT host
       mosh [--wisp URL] host MOSH CONNECT PORT KEY

Connects to a mosh-server. Its UDP datagrams travel over a Wisp server with
UDP support (skywire cli wisp serve, or any Wisp v2 server with the UDP
extension), which must be able to reach host.

Like real mosh, the first form logs in with ssh (through the same Wisp
server; see ssh --help), runs mosh-server there, and connects to the port
and key it prints. With --key, or a pasted "MOSH CONNECT PORT KEY" line
from a mosh-server you started yourself, there is no ssh step.

  --wisp URL       Wisp endpoint, ws:// or wss://   (default $WISP_URL)
  --key KEY        session key                       (default $MOSH_KEY)
  --port PORT      mosh-server's UDP port (with ssh: the port it should use)
  --ssh-port PORT  ssh port for the login            (default 22)

Quit with Ctrl-^ then '.'.
`

// Register adds the mosh command to the shell.
func Register() {
	shell.RegisterApplet("mosh", "mobile shell over a Wisp UDP stream (mosh --help)", runMosh)
}

type args struct {
	wisp, key, user, host string
	port, sshPort         uint16
}

// bootstrap reports whether the session needs an ssh login to start
// mosh-server: no key was given.
func (a args) bootstrap() bool { return a.key == "" }

func parseArgs(argv []string, env func(string) string) (args, error) {
	a := args{key: env("MOSH_KEY")}
	wispFlag := ""
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
		case "wisp", "key", "port", "ssh-port", "p", "k", "w":
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
			wispFlag = val
		case "key", "k":
			a.key = val
		case "port", "p", "ssh-port":
			p, err := parsePort(val)
			if err != nil {
				return a, err
			}
			if name == "ssh-port" {
				a.sshPort = p
			} else {
				a.port = p
			}
		}
	}
	if len(pos) == 0 {
		return a, errHelp
	}
	a.host = pos[0]
	if i := strings.LastIndexByte(a.host, '@'); i >= 0 {
		a.user, a.host = a.host[:i], a.host[i+1:]
	}
	if a.user == "" {
		a.user = env("USER")
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
	var err error
	if a.wisp, err = shell.WispURL(wispFlag, env); err != nil {
		return a, err
	}
	if !a.bootstrap() && a.port == 0 {
		return a, errors.New("no port: give --port, from mosh-server's MOSH CONNECT line")
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

// locale is what real mosh hands mosh-server with -l: the locale variables
// that are set here, or a UTF-8 LANG when none is.
func locale(env func(string) string) []string {
	var out []string
	for _, name := range []string{"LANG", "LANGUAGE", "LC_CTYPE", "LC_ALL"} {
		if v := env(name); v != "" {
			out = append(out, name+"="+v)
		}
	}
	if len(out) == 0 {
		out = []string{"LANG=en_US.UTF-8"}
	}
	return out
}

// colors is what tput colors would say for TERM.
func colors(term string) int {
	switch {
	case strings.Contains(term, "256color"):
		return 256
	case term == "" || term == "dumb":
		return 0
	}
	return 8
}

// startServer logs in with ssh, starts mosh-server and returns its port and
// key, the way the mosh script does before it hands over to mosh-client.
func startServer(ctx context.Context, ap ssh.Applet, a args) (uint16, string, error) {
	c, err := ssh.Dial(ctx, ap.Options(a.wisp, a.user, a.host, a.sshPort, nil))
	if err != nil {
		return 0, "", err
	}
	defer c.Close() //nolint:errcheck // done with ssh once mosh-server is up
	term := ap.Env("TERM")
	return c.StartMoshServer(ssh.MoshServerCommand(colors(term), a.port, locale(ap.Env)), term)
}

func runMosh(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, argv []string) int {
	ap := ssh.Applet{S: s, HC: hc}
	a, err := parseArgs(argv, ap.Env)
	if errors.Is(err, errHelp) {
		shell.Print(hc.Stdout, usage)
		return 0
	}
	if err != nil {
		shell.Printf(hc.Stderr, "mosh: %v\n", err)
		return 1
	}
	defer ap.Raw()()
	if a.bootstrap() {
		if a.port, a.key, err = startServer(ctx, ap, a); err != nil {
			shell.Printf(hc.Stderr, "mosh: %v\n", err)
			return 1
		}
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
