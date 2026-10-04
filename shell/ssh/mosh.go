package ssh

import (
	"bytes"
	"errors"
	"strconv"
	"strings"
	"sync"

	gossh "golang.org/x/crypto/ssh"
)

// MoshServerCommand is the remote command the mosh script runs over ssh:
// "mosh-server new -c COLORS -s [-p PORT] -l NAME=VALUE...". -s binds the
// server to the address the ssh connection arrived on.
func MoshServerCommand(colors int, port uint16, locale []string) string {
	args := []string{"mosh-server", "new", "-c", strconv.Itoa(colors), "-s"}
	if port != 0 {
		args = append(args, "-p", strconv.Itoa(int(port)))
	}
	for _, l := range locale {
		args = append(args, "-l", shellQuote(l))
	}
	return strings.Join(args, " ")
}

// shellQuote quotes s for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.=:/@,+") == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// StartMoshServer runs command (see MoshServerCommand) on a pty, as mosh's
// ssh -tt does, and returns the port and key from its MOSH CONNECT line.
func (c *Client) StartMoshServer(command, term string) (port uint16, key string, err error) {
	sess, err := c.NewSession()
	if err != nil {
		return 0, "", err
	}
	defer sess.Close() //nolint:errcheck // the session is over either way
	if term == "" {
		term = "xterm-256color"
	}
	if err := sess.RequestPty(term, 24, 80, gossh.TerminalModes{gossh.ECHO: 0}); err != nil {
		return 0, "", err
	}
	var out bytes.Buffer
	w := &lockedWriter{mu: new(sync.Mutex), w: &out}
	sess.Stdout, sess.Stderr = w, w
	runErr := sess.Run(command)
	port, key, err = ParseMoshConnect(out.Bytes())
	if err != nil {
		msg := strings.TrimSpace(strings.ReplaceAll(out.String(), "\r", ""))
		if runErr != nil && msg == "" {
			msg = runErr.Error()
		}
		if msg != "" {
			err = errors.New("did not find mosh-server startup message: " + msg)
		}
	}
	return port, key, err
}

// ParseMoshConnect finds "MOSH CONNECT <port> <key>" in mosh-server's output.
func ParseMoshConnect(out []byte) (port uint16, key string, err error) {
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r", "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "MOSH" || f[1] != "CONNECT" {
			continue
		}
		p, perr := strconv.ParseUint(f[2], 10, 16)
		if perr != nil || p == 0 {
			return 0, "", errors.New("bad port in MOSH CONNECT line: " + strconv.Quote(f[2]))
		}
		return uint16(p), f[3], nil
	}
	return 0, "", errors.New("did not find mosh-server startup message (is mosh installed on the server?)")
}
