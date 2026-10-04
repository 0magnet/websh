// Package ssh is an ssh client for websh. A page cannot open a TCP
// connection, so the connection is a Wisp TCP stream: a Wisp server
// (skywire cli wisp serve, or wisp.NewServer) dials the host, and the
// protocol is golang.org/x/crypto/ssh.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"path"
	"strconv"
	"strings"
	"sync"

	"github.com/0magnet/afero"
	"github.com/0magnet/wisp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// Options says where to connect and how to authenticate.
type Options struct {
	// WispURL is the ws:// or wss:// endpoint of the Wisp server that makes
	// the TCP connection.
	WispURL string
	// User, Host and Port are the ssh destination as the Wisp server sees
	// it. Port 0 means 22.
	User string
	Host string
	Port uint16

	// FS holds the keys and known_hosts; Home is the directory whose .ssh
	// they are in.
	FS   afero.Fs
	Home string
	// Identities are private key files to offer before the defaults
	// (~/.ssh/id_ed25519, id_ecdsa, id_rsa), as absolute paths on FS.
	Identities []string

	// In and Out are the terminal, in raw mode, for the prompts: password,
	// passphrase, and whether to trust an unknown host key.
	In  io.Reader
	Out io.Writer
}

// Client is an ssh connection over a Wisp stream.
type Client struct {
	*gossh.Client
	wisp *wisp.Client
}

// Close ends the ssh connection and the Wisp session under it.
func (c *Client) Close() error {
	err := c.Client.Close()
	c.wisp.Close() //nolint:errcheck,gosec // the ssh error is the one that matters
	return err
}

var defaultIdentities = []string{"id_ed25519", "id_ecdsa", "id_rsa"}

// Dial connects and authenticates.
func Dial(ctx context.Context, o Options) (*Client, error) {
	if o.WispURL == "" {
		return nil, errors.New("no Wisp server: give --wisp ws://HOST/PATH or set WISP_URL")
	}
	if o.Port == 0 {
		o.Port = 22
	}
	t := &tty{in: o.In, out: o.Out}
	kh, err := loadKnownHosts(o.FS, path.Join(o.Home, ".ssh", "known_hosts"))
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(o.Host, strconv.Itoa(int(o.Port)))
	display := displayHost(o.Host, o.Port)

	keys := &identities{t: t, fs: o.FS}
	for _, f := range o.Identities {
		keys.load(f, true)
	}
	for _, f := range defaultIdentities {
		keys.load(path.Join(o.Home, ".ssh", f), false)
	}

	cfg := &gossh.ClientConfig{
		User:              o.User,
		HostKeyCallback:   kh.hostKeyCheck(t, display),
		HostKeyAlgorithms: kh.algorithms(knownhosts.Normalize(addr)),
		Auth: []gossh.AuthMethod{
			gossh.PublicKeysCallback(keys.signers),
			gossh.RetryableAuthMethod(gossh.KeyboardInteractive(func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				if name != "" {
					t.print(name + "\n")
				}
				if instruction != "" {
					t.print(instruction + "\n")
				}
				answers := make([]string, len(questions))
				for i, q := range questions {
					a, err := t.readLine(q, echos[i])
					if err != nil {
						return nil, err
					}
					answers[i] = a
				}
				return answers, nil
			}), 3),
			gossh.RetryableAuthMethod(gossh.PasswordCallback(func() (string, error) {
				return t.password(o.User + "@" + o.Host + "'s password: ")
			}), 3),
		},
		BannerCallback: func(msg string) error { t.print(msg); return nil },
	}

	wc, err := wisp.Dial(ctx, wisp.ClientConfig{URL: o.WispURL})
	if err != nil {
		return nil, fmt.Errorf("wisp: %w", err)
	}
	conn, err := wc.DialContext(ctx, "tcp", addr)
	if err != nil {
		wc.Close() //nolint:errcheck,gosec // reporting the real problem
		return nil, fmt.Errorf("connect to %s: %w", display, err)
	}
	stop := context.AfterFunc(ctx, func() { conn.Close() }) //nolint:errcheck,gosec // unblocks the handshake
	sc, chans, reqs, err := gossh.NewClientConn(conn, addr, cfg)
	stop()
	if err != nil {
		conn.Close() //nolint:errcheck,gosec // reporting the real problem
		wc.Close()   //nolint:errcheck,gosec // reporting the real problem
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, tidyAuthError(err, o.User, display)
	}
	return &Client{Client: gossh.NewClient(sc, chans, reqs), wisp: wc}, nil
}

func tidyAuthError(err error, user, display string) error {
	if msg := err.Error(); strings.HasPrefix(msg, "ssh: handshake failed: ") {
		err = errors.New(strings.TrimPrefix(msg, "ssh: handshake failed: "))
	}
	if strings.Contains(err.Error(), "unable to authenticate") {
		return fmt.Errorf("%s@%s: permission denied", user, display)
	}
	if errors.Is(err, errInterrupted) || strings.Contains(err.Error(), errInterrupted.Error()) {
		return errInterrupted
	}
	return err
}

// identities loads private keys lazily: an encrypted key is offered by its
// public half, and the passphrase is asked for only once the server says it
// would accept that key.
type identities struct {
	t    *tty
	fs   afero.Fs
	seen map[string]bool
	list []gossh.Signer
}

func (ids *identities) load(file string, explicit bool) {
	if ids.seen == nil {
		ids.seen = map[string]bool{}
	}
	if ids.seen[file] {
		return
	}
	ids.seen[file] = true
	pem, err := afero.ReadFile(ids.fs, file)
	if err != nil {
		if explicit {
			ids.t.print("Warning: Identity file " + file + " not accessible: " + err.Error() + ".\n")
		}
		return
	}
	s, err := gossh.ParsePrivateKey(pem)
	var missing *gossh.PassphraseMissingError
	switch {
	case err == nil:
		ids.list = append(ids.list, s)
	case errors.As(err, &missing) && missing.PublicKey != nil:
		ids.list = append(ids.list, &lockedKey{t: ids.t, file: file, pem: pem, pub: missing.PublicKey})
	case errors.As(err, &missing):
		// An old-format key that does not carry its public half: ask now.
		if s := unlock(ids.t, file, pem); s != nil {
			ids.list = append(ids.list, s)
		}
	default:
		ids.t.print("Load key \"" + file + "\": " + err.Error() + "\n")
	}
}

func (ids *identities) signers() ([]gossh.Signer, error) { return ids.list, nil }

// unlock asks for a passphrase until it opens the key, up to three times; an
// empty answer skips the key.
func unlock(t *tty, file string, pem []byte) gossh.Signer {
	for range 3 {
		pass, err := t.password("Enter passphrase for key '" + file + "': ")
		if err != nil || pass == "" {
			return nil
		}
		s, err := gossh.ParsePrivateKeyWithPassphrase(pem, []byte(pass))
		if err == nil {
			return s
		}
		t.print("Bad passphrase, try again for " + file + ".\n")
	}
	return nil
}

// lockedKey is an encrypted private key known by its public half.
type lockedKey struct {
	t    *tty
	file string
	pem  []byte
	pub  gossh.PublicKey

	once   sync.Once
	signer gossh.Signer
}

var _ gossh.AlgorithmSigner = (*lockedKey)(nil)

func (k *lockedKey) PublicKey() gossh.PublicKey { return k.pub }

func (k *lockedKey) open() (gossh.Signer, error) {
	k.once.Do(func() { k.signer = unlock(k.t, k.file, k.pem) })
	if k.signer == nil {
		return nil, errors.New("no passphrase for " + k.file)
	}
	return k.signer, nil
}

func (k *lockedKey) Sign(rand io.Reader, data []byte) (*gossh.Signature, error) {
	s, err := k.open()
	if err != nil {
		return nil, err
	}
	return s.Sign(rand, data)
}

func (k *lockedKey) SignWithAlgorithm(rand io.Reader, data []byte, algorithm string) (*gossh.Signature, error) {
	s, err := k.open()
	if err != nil {
		return nil, err
	}
	as, ok := s.(gossh.AlgorithmSigner)
	if !ok {
		return s.Sign(rand, data)
	}
	return as.SignWithAlgorithm(rand, data, algorithm)
}
