package ssh

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
)

const usage = `usage: ssh [--wisp URL] [-n] [-p port] [-i keyfile] [-l user] [user@]host [command]

An ssh client. A page cannot open a TCP connection, so ssh only works
through a Wisp server — skywire cli wisp serve, or wisp.NewServer — and it
is the Wisp server that connects to host.

  --wisp URL    Wisp endpoint, ws:// or wss://   (default $WISP_URL)
  -p port       ssh port                         (default 22)
  -i keyfile    private key to offer, before ~/.ssh/id_ed25519, id_ecdsa, id_rsa
  -l user       remote user                      (default $USER)
  -n            with a command: do not send it stdin

Host keys are checked against ~/.ssh/known_hosts. Without a command ssh
opens a login shell on a pty; ~. at the start of a line disconnects.
`

// Register adds the ssh command to the shell.
func Register() {
	shell.RegisterApplet("ssh", "ssh client over a Wisp TCP stream (ssh --help)", runSSH)
}

type args struct {
	wisp, user, host string
	port             uint16
	identities       []string
	noStdin          bool
	command          string
}

var errHelp = errors.New("help")

func parseArgs(argv []string, env func(string) string) (args, error) {
	var a args
	wispFlag := ""
	for len(argv) > 0 && strings.HasPrefix(argv[0], "-") && argv[0] != "-" {
		arg := argv[0]
		argv = argv[1:]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--") {
			name, val, hasVal := strings.Cut(arg[2:], "=")
			switch name {
			case "help":
				return a, errHelp
			case "wisp":
				if !hasVal {
					if len(argv) == 0 {
						return a, errors.New("option --wisp needs a value")
					}
					val, argv = argv[0], argv[1:]
				}
				wispFlag = val
			default:
				return a, errors.New("unknown option " + arg)
			}
			continue
		}
		for i := 1; i < len(arg); i++ {
			switch c := arg[i]; c {
			case 'h':
				return a, errHelp
			case 'n':
				a.noStdin = true
			case 't', 'T', 'q', 'v', 'C', 'A', 'x', 'X':
				// accepted for muscle memory; nothing to do
			case 'p', 'i', 'l':
				val := arg[i+1:]
				if val == "" {
					if len(argv) == 0 {
						return a, errors.New("option -" + string(c) + " needs a value")
					}
					val, argv = argv[0], argv[1:]
				}
				switch c {
				case 'p':
					p, err := strconv.ParseUint(val, 10, 16)
					if err != nil || p == 0 {
						return a, errors.New("bad port " + strconv.Quote(val))
					}
					a.port = uint16(p)
				case 'i':
					a.identities = append(a.identities, val)
				case 'l':
					a.user = val
				}
				i = len(arg)
			default:
				return a, errors.New("unknown option -" + string(c))
			}
		}
	}
	if len(argv) == 0 {
		return a, errHelp
	}
	dest := argv[0]
	if u, h, ok := strings.Cut(dest, "@"); ok {
		if a.user == "" {
			a.user = u
		}
		dest = h
	}
	a.host = strings.TrimSuffix(strings.TrimPrefix(dest, "["), "]")
	if a.host == "" {
		return a, errors.New("no host")
	}
	if a.user == "" {
		a.user = env("USER")
	}
	a.command = strings.Join(argv[1:], " ")
	var err error
	a.wisp, err = shell.WispURL(wispFlag, env)
	return a, err
}

// Applet is how ssh and mosh reach the shell they run in.
type Applet struct {
	S  *shell.Shell
	HC *interp.HandlerContext
}

// Env reads a shell variable.
func (ap Applet) Env(name string) string { return ap.HC.Env.Get(name).String() }

// Options fills in what the shell knows: the filesystem, $HOME, and the
// terminal for prompts. identities are resolved against the working
// directory.
func (ap Applet) Options(wispURL, user, host string, port uint16, identities []string) Options {
	o := Options{
		WispURL: wispURL, User: user, Host: host, Port: port,
		FS: ap.S.FS, Home: ap.Env("HOME"),
		In: ap.HC.Stdin, Out: ap.HC.Stderr,
	}
	if o.Home == "" {
		o.Home = "/"
	}
	for _, f := range identities {
		o.Identities = append(o.Identities, shell.Resolve(ap.HC, f))
	}
	return o
}

// Raw puts the terminal in raw mode and returns the undo.
func (ap Applet) Raw() func() {
	if ap.S.RawMode == nil {
		return func() {}
	}
	ap.S.RawMode(true)
	return func() { ap.S.RawMode(false) }
}

func runSSH(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, argv []string) int {
	ap := Applet{S: s, HC: hc}
	defer s.WithSource("remote")() // what arrives is another machine's
	a, err := parseArgs(argv, ap.Env)
	if errors.Is(err, errHelp) {
		shell.Print(hc.Stdout, usage)
		return 0
	}
	if err != nil {
		shell.Printf(hc.Stderr, "ssh: %v\n", err)
		return 255
	}

	// Raw for the prompts; a command then runs with the terminal cooked,
	// so what is typed for it is echoed and sent a line at a time.
	cooked := ap.Raw()
	c, err := Dial(ctx, ap.Options(a.wisp, a.user, a.host, a.port, a.identities))
	if err != nil {
		cooked()
		shell.Printf(hc.Stderr, "ssh: %v\n", err)
		return 255
	}
	defer c.Close() //nolint:errcheck // the session is over either way

	t := Terminal{In: hc.Stdin, Out: hc.Stdout, Err: hc.Stderr, Term: ap.Env("TERM"), Size: s.Size, Wake: s.WakeStdin}
	var code int
	if a.command == "" {
		code, err = c.Shell(ctx, t)
		cooked()
		if err == nil && code == 255 {
			shell.Printf(hc.Stderr, "Connection to %s closed.\n", a.host)
		}
	} else {
		cooked()
		code, err = c.Run(ctx, a.command, t, a.noStdin)
	}
	if err != nil {
		shell.Printf(hc.Stderr, "ssh: %v\n", err)
	}
	return code
}
