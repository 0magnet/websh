//go:build !js

package ssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0magnet/afero"
	"github.com/0magnet/wisp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// testServer is an in-process sshd reached through an in-process Wisp server.
type testServer struct {
	wispURL string
	port    uint16
	hostKey gossh.Signer

	mu       sync.Mutex
	commands []string
	ptyTerm  string
}

func newKey(t *testing.T) (ed25519.PrivateKey, gossh.Signer) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return priv, s
}

// startServer accepts user "alice" with password "secret", or with any key
// in authorized.
func startServer(t *testing.T, authorized ...gossh.PublicKey) *testServer {
	t.Helper()
	ts := &testServer{}
	_, ts.hostKey = newKey(t)
	cfg := &gossh.ServerConfig{
		PasswordCallback: func(c gossh.ConnMetadata, pw []byte) (*gossh.Permissions, error) {
			if c.User() == "alice" && string(pw) == "secret" {
				return nil, nil
			}
			return nil, io.ErrUnexpectedEOF
		},
		PublicKeyCallback: func(c gossh.ConnMetadata, k gossh.PublicKey) (*gossh.Permissions, error) {
			for _, a := range authorized {
				if bytes.Equal(a.Marshal(), k.Marshal()) {
					return nil, nil
				}
			}
			return nil, io.ErrUnexpectedEOF
		},
	}
	cfg.AddHostKey(ts.hostKey)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })                //nolint:errcheck,gosec // test teardown
	ts.port = uint16(ln.Addr().(*net.TCPAddr).Port) //nolint:gosec // a port fits
	go func() {
		for {
			nc, err := ln.Accept()
			if err != nil {
				return
			}
			go ts.serve(nc, cfg)
		}
	}()

	ws, err := wisp.NewServer(wisp.Config{Egress: &wisp.DirectEgress{}})
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(ws)
	t.Cleanup(hs.Close)
	ts.wispURL = "ws" + strings.TrimPrefix(hs.URL, "http")
	return ts
}

func (ts *testServer) serve(nc net.Conn, cfg *gossh.ServerConfig) {
	_, chans, reqs, err := gossh.NewServerConn(nc, cfg)
	if err != nil {
		return
	}
	go gossh.DiscardRequests(reqs)
	for nch := range chans {
		if nch.ChannelType() != "session" {
			nch.Reject(gossh.UnknownChannelType, "no") //nolint:errcheck,gosec // test server
			continue
		}
		ch, reqs, err := nch.Accept()
		if err != nil {
			return
		}
		go ts.session(ch, reqs)
	}
}

func exit(ch gossh.Channel, code uint32) {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], code)
	ch.SendRequest("exit-status", false, b[:]) //nolint:errcheck,gosec // test server
	ch.Close()                                 //nolint:errcheck,gosec // test server
}

// session runs "echo X", "exit N", "cat", "mosh-server ..." and a shell that
// echoes what is typed and exits on "exit\r".
func (ts *testServer) session(ch gossh.Channel, reqs <-chan *gossh.Request) {
	for req := range reqs {
		switch req.Type {
		case "pty-req":
			n := binary.BigEndian.Uint32(req.Payload)
			ts.mu.Lock()
			ts.ptyTerm = string(req.Payload[4 : 4+n])
			ts.mu.Unlock()
			req.Reply(true, nil) //nolint:errcheck,gosec // test server
		case "shell":
			req.Reply(true, nil) //nolint:errcheck,gosec // test server
			go func() {
				io.WriteString(ch, "welcome\r\n") //nolint:errcheck,gosec // test server
				var typed []byte
				buf := make([]byte, 256)
				for {
					n, err := ch.Read(buf)
					ch.Write(buf[:n]) //nolint:errcheck,gosec // the pty's echo
					typed = append(typed, buf[:n]...)
					if bytes.HasSuffix(typed, []byte("exit\r")) {
						exit(ch, 0)
						return
					}
					if err != nil {
						exit(ch, 1)
						return
					}
				}
			}()
		case "exec":
			n := binary.BigEndian.Uint32(req.Payload)
			cmd := string(req.Payload[4 : 4+n])
			ts.mu.Lock()
			ts.commands = append(ts.commands, cmd)
			ts.mu.Unlock()
			req.Reply(true, nil) //nolint:errcheck,gosec // test server
			go func() {
				switch f := strings.Fields(cmd); {
				case f[0] == "echo":
					io.WriteString(ch, strings.Join(f[1:], " ")+"\n") //nolint:errcheck,gosec // test server
					exit(ch, 0)
				case f[0] == "exit":
					code, err := strconv.Atoi(f[1])
					if err != nil {
						code = 255
					}
					io.WriteString(ch.Stderr(), "bye\n") //nolint:errcheck,gosec // test server
					exit(ch, uint32(code))               //nolint:gosec // small
				case f[0] == "cat":
					io.Copy(ch, ch) //nolint:errcheck,gosec // test server
					exit(ch, 0)
				case f[0] == "mosh-server":
					io.WriteString(ch, "\r\nMOSH CONNECT 60001 4NeCCgvZFe2RnPgrcU1PQw\r\n\r\nmosh-server (mosh 1.4.0) [build mosh 1.4.0]\r\n") //nolint:errcheck,gosec // test server
					exit(ch, 0)
				default:
					exit(ch, 127)
				}
			}()
		default:
			req.Reply(req.Type == "window-change", nil) //nolint:errcheck,gosec // test server
		}
	}
}

func (ts *testServer) opts(fs afero.Fs, in string, out *bytes.Buffer) Options {
	return Options{
		WispURL: ts.wispURL, User: "alice", Host: "127.0.0.1", Port: ts.port,
		FS: fs, Home: "/home/user",
		In: strings.NewReader(in), Out: out,
	}
}

func (ts *testServer) knownHostsLine() string {
	return knownhosts.Line([]string{knownhosts.Normalize(net.JoinHostPort("127.0.0.1", strconv.Itoa(int(ts.port))))}, ts.hostKey.PublicKey()) + "\n"
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestPasswordAndUnknownHost(t *testing.T) {
	ts := startServer(t)
	fs := afero.NewMemMapFs()
	var out bytes.Buffer
	c, err := Dial(ctx(t), ts.opts(fs, "maybe\nyes\nsecret\n", &out))
	if err != nil {
		t.Fatalf("Dial: %v\n%s", err, out.String())
	}
	defer c.Close() //nolint:errcheck // test
	for _, want := range []string{"can't be established", gossh.FingerprintSHA256(ts.hostKey.PublicKey()), "Please type 'yes' or 'no'", "Permanently added", "alice@127.0.0.1's password: "} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("prompts lack %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "secret") {
		t.Error("the password was echoed")
	}
	kh, err := afero.ReadFile(fs, "/home/user/.ssh/known_hosts")
	if err != nil || string(kh) != ts.knownHostsLine() {
		t.Fatalf("known_hosts = %q, %v; want %q", kh, err, ts.knownHostsLine())
	}

	var stdout bytes.Buffer
	code, err := c.Run(ctx(t), "echo hi there", Terminal{In: strings.NewReader(""), Out: &stdout}, false)
	if err != nil || code != 0 || stdout.String() != "hi there\n" {
		t.Fatalf("echo: code %d err %v out %q", code, err, stdout.String())
	}

	// Known now: no question the second time.
	out.Reset()
	c2, err := Dial(ctx(t), ts.opts(fs, "secret\n", &out))
	if err != nil {
		t.Fatalf("second Dial: %v\n%s", err, out.String())
	}
	c2.Close() //nolint:errcheck,gosec // test
	if strings.Contains(out.String(), "authenticity") {
		t.Errorf("asked about a known host:\n%s", out.String())
	}

	// A wrong password three times is a refusal.
	out.Reset()
	if _, err := Dial(ctx(t), ts.opts(fs, "a\nb\nc\n", &out)); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("bad password: %v", err)
	}
}

func TestRefuseUnknownHost(t *testing.T) {
	ts := startServer(t)
	fs := afero.NewMemMapFs()
	var out bytes.Buffer
	if _, err := Dial(ctx(t), ts.opts(fs, "no\n", &out)); err == nil || !strings.Contains(err.Error(), "host key verification failed") {
		t.Fatalf("Dial = %v", err)
	}
	if ok, err := afero.Exists(fs, "/home/user/.ssh/known_hosts"); ok || err != nil {
		t.Error("a refused key was recorded")
	}
}

func TestHostKeyMismatch(t *testing.T) {
	ts := startServer(t)
	fs := afero.NewMemMapFs()
	_, other := newKey(t)
	line := knownhosts.Line([]string{knownhosts.Normalize(net.JoinHostPort("127.0.0.1", strconv.Itoa(int(ts.port))))}, other.PublicKey())
	afero.WriteFile(fs, "/home/user/.ssh/known_hosts", []byte("# comment\n"+line+"\n"), 0o600) //nolint:errcheck,gosec // test
	var out bytes.Buffer
	_, err := Dial(ctx(t), ts.opts(fs, "yes\nsecret\n", &out))
	if err == nil || !strings.Contains(err.Error(), "host key verification failed") {
		t.Fatalf("Dial = %v", err)
	}
	for _, want := range []string{"REMOTE HOST IDENTIFICATION HAS CHANGED", gossh.FingerprintSHA256(ts.hostKey.PublicKey()), "known_hosts:2"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("warning lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "password") {
		t.Error("went on to authenticate")
	}
}

func TestPublicKeyAuth(t *testing.T) {
	priv, signer := newKey(t)
	encPriv, encSigner := newKey(t)
	ts := startServer(t, signer.PublicKey(), encSigner.PublicKey())
	fs := afero.NewMemMapFs()
	afero.WriteFile(fs, "/home/user/.ssh/known_hosts", []byte(ts.knownHostsLine()), 0o600) //nolint:errcheck,gosec // test

	// The default identity, unencrypted: no prompts at all.
	block, err := gossh.MarshalPrivateKey(priv, "")
	if err != nil {
		t.Fatal(err)
	}
	afero.WriteFile(fs, "/home/user/.ssh/id_ed25519", pem.EncodeToMemory(block), 0o600) //nolint:errcheck,gosec // test
	var out bytes.Buffer
	c, err := Dial(ctx(t), ts.opts(fs, "", &out))
	if err != nil {
		t.Fatalf("Dial: %v\n%s", err, out.String())
	}
	c.Close() //nolint:errcheck,gosec // test
	if out.Len() != 0 {
		t.Errorf("unexpected output: %q", out.String())
	}

	// -i with an encrypted key: the passphrase, asked once, retried once.
	fs.Remove("/home/user/.ssh/id_ed25519") //nolint:errcheck,gosec // test
	block, err = gossh.MarshalPrivateKeyWithPassphrase(encPriv, "", []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	afero.WriteFile(fs, "/home/user/keys/enc", pem.EncodeToMemory(block), 0o600) //nolint:errcheck,gosec // test
	out.Reset()
	o := ts.opts(fs, "wrong\nhunter2\n", &out)
	o.Identities = []string{"/home/user/keys/enc"}
	c, err = Dial(ctx(t), o)
	if err != nil {
		t.Fatalf("Dial with -i: %v\n%s", err, out.String())
	}
	c.Close() //nolint:errcheck,gosec // test
	if got := strings.Count(out.String(), "Enter passphrase for key '/home/user/keys/enc'"); got != 2 {
		t.Errorf("asked %d times:\n%s", got, out.String())
	}
	if strings.Contains(out.String(), "password") {
		t.Errorf("fell back to a password:\n%s", out.String())
	}
}

func TestRunStatusAndStdin(t *testing.T) {
	ts := startServer(t)
	fs := afero.NewMemMapFs()
	afero.WriteFile(fs, "/home/user/.ssh/known_hosts", []byte(ts.knownHostsLine()), 0o600) //nolint:errcheck,gosec // test
	var out bytes.Buffer
	c, err := Dial(ctx(t), ts.opts(fs, "secret\n", &out))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close() //nolint:errcheck // test

	var stdout, stderr bytes.Buffer
	code, err := c.Run(ctx(t), "exit 3", Terminal{In: strings.NewReader(""), Out: &stdout, Err: &stderr}, true)
	if err != nil || code != 3 || stderr.String() != "bye\n" {
		t.Fatalf("exit 3: code %d err %v stderr %q", code, err, stderr.String())
	}
	stdout.Reset()
	code, err = c.Run(ctx(t), "cat", Terminal{In: strings.NewReader("piped\ninput\n"), Out: &stdout}, false)
	if err != nil || code != 0 || stdout.String() != "piped\ninput\n" {
		t.Fatalf("cat: code %d err %v out %q", code, err, stdout.String())
	}
}

// syncBuffer is a bytes.Buffer safe for the session's output copier.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestInteractiveShell(t *testing.T) {
	ts := startServer(t)
	fs := afero.NewMemMapFs()
	afero.WriteFile(fs, "/home/user/.ssh/known_hosts", []byte(ts.knownHostsLine()), 0o600) //nolint:errcheck,gosec // test
	var out bytes.Buffer
	c, err := Dial(ctx(t), ts.opts(fs, "secret\n", &out))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close() //nolint:errcheck // test

	run := func(keys string) (int, string) {
		pr, pw := io.Pipe()
		var term syncBuffer
		type result struct {
			code int
			err  error
		}
		res := make(chan result, 1)
		go func() {
			code, err := c.Shell(ctx(t), Terminal{
				In: pr, Out: &term, Term: "xterm-256color",
				Size: func() (int, int) { return 100, 30 },
				// As web.Session does it: an empty write is a zero-byte Read.
				Wake: func() { go pw.Write(nil) }, //nolint:errcheck // test
			})
			res <- result{code, err}
		}()
		pw.Write([]byte(keys)) //nolint:errcheck,gosec // test input
		select {
		case r := <-res:
			if r.err != nil {
				t.Fatalf("Shell: %v", r.err)
			}
			return r.code, term.String()
		case <-time.After(10 * time.Second):
			t.Fatalf("Shell did not return; screen %q", term.String())
		}
		return 0, ""
	}

	// The remote shell exits: Shell returns without another key.
	code, screen := run("echo ~~x\rexit\r")
	if code != 0 || !strings.Contains(screen, "welcome") || !strings.Contains(screen, "echo ~~x\rexit\r") {
		t.Fatalf("exit: code %d screen %q", code, screen)
	}
	ts.mu.Lock()
	term := ts.ptyTerm
	ts.mu.Unlock()
	if term != "xterm-256color" {
		t.Errorf("pty TERM = %q", term)
	}

	// ~. at the start of a line disconnects.
	if code, screen = run("ls\r~."); code != 255 {
		t.Fatalf("~.: code %d screen %q", code, screen)
	}
}

func TestMoshServerBootstrap(t *testing.T) {
	ts := startServer(t)
	fs := afero.NewMemMapFs()
	afero.WriteFile(fs, "/home/user/.ssh/known_hosts", []byte(ts.knownHostsLine()), 0o600) //nolint:errcheck,gosec // test
	var out bytes.Buffer
	c, err := Dial(ctx(t), ts.opts(fs, "secret\n", &out))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close() //nolint:errcheck // test

	cmd := MoshServerCommand(256, 0, []string{"LANG=en_US.UTF-8"})
	if cmd != "mosh-server new -c 256 -s -l LANG=en_US.UTF-8" {
		t.Errorf("command = %q", cmd)
	}
	port, key, err := c.StartMoshServer(cmd, "xterm-256color")
	if err != nil || port != 60001 || key != "4NeCCgvZFe2RnPgrcU1PQw" {
		t.Fatalf("StartMoshServer = %d %q %v", port, key, err)
	}
	ts.mu.Lock()
	got := ts.commands
	ts.mu.Unlock()
	if len(got) != 1 || got[0] != cmd {
		t.Errorf("server ran %q", got)
	}

	if _, _, err := c.StartMoshServer("nosuch", ""); err == nil {
		t.Error("no MOSH CONNECT line accepted")
	}
	if got := MoshServerCommand(8, 60010, []string{"LC_ALL=it's"}); got != `mosh-server new -c 8 -s -p 60010 -l 'LC_ALL=it'\''s'` {
		t.Errorf("quoted command = %q", got)
	}
}

func TestHostPatterns(t *testing.T) {
	hashed := knownhosts.HashHostname("example.net")
	for _, tc := range []struct {
		patterns []string
		host     string
		want     bool
	}{
		{[]string{"example.net"}, "example.net", true},
		{[]string{"other", "Example.NET"}, "example.net", true},
		{[]string{"*.example.net"}, "a.example.net", true},
		{[]string{"*.example.net", "!bad.example.net"}, "bad.example.net", false},
		{[]string{"[example.net]:2222"}, "[example.net]:2222", true},
		{[]string{"[example.net]:2222"}, "example.net", false},
		{[]string{"host?"}, "host1", true},
		{[]string{hashed}, "example.net", true},
		{[]string{hashed}, "example.org", false},
	} {
		if got := hostsMatch(tc.patterns, tc.host); got != tc.want {
			t.Errorf("hostsMatch(%q, %q) = %v", tc.patterns, tc.host, got)
		}
	}
}

func TestParseArgs(t *testing.T) {
	env := func(k string) string { return map[string]string{"USER": "user", "WISP_URL": "ws://w/"}[k] }
	a, err := parseArgs([]string{"-p", "2222", "-i", "k", "-ik2", "bob@h", "uname", "-a"}, env)
	if err != nil || a.user != "bob" || a.host != "h" || a.port != 2222 || a.command != "uname -a" ||
		len(a.identities) != 2 || a.identities[1] != "k2" || a.wisp != "ws://w/" {
		t.Fatalf("%+v %v", a, err)
	}
	a, err = parseArgs([]string{"-l", "carol", "--wisp=ws://x/", "-n", "h"}, env)
	if err != nil || a.user != "carol" || a.wisp != "ws://x/" || !a.noStdin || a.command != "" {
		t.Fatalf("%+v %v", a, err)
	}
	if a, err = parseArgs([]string{"h"}, env); err != nil || a.user != "user" {
		t.Errorf("default user %q %v", a.user, err)
	}
	if _, err := parseArgs([]string{"h"}, func(string) string { return "" }); err == nil {
		t.Error("no Wisp URL accepted")
	}
	if _, err := parseArgs(nil, env); err != errHelp {
		t.Errorf("no args = %v", err)
	}
}

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }

func TestEscapes(t *testing.T) {
	for _, tc := range []struct {
		in, sent string
		quit     bool
	}{
		{"a~.b\r~~x\r~.", "a~.b\r~x\r", true},
		{"~x", "~x", false},
		{"\n~.", "\n", true},
	} {
		var sent bytes.Buffer
		quit := forwardKeys(strings.NewReader(tc.in), nopCloser{&sent}, make(chan struct{}))
		if quit != tc.quit || sent.String() != tc.sent {
			t.Errorf("%q: sent %q quit %v", tc.in, sent.String(), quit)
		}
	}
}
