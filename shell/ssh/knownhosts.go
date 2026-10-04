package ssh

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // known_hosts hashes host names with HMAC-SHA1
	"encoding/base64"
	"errors"
	"fmt"
	iofs "io/fs"
	"net"
	"path"
	"strconv"
	"strings"

	"github.com/0magnet/afero"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// knownhosts.New reads files through the os package, which has nothing
// behind it in a browser; the file here lives on websh's afero filesystem. So
// the parsing is ssh.ParseKnownHosts and the matching is done here, while
// knownhosts supplies the canonical host names and the lines written back.

// hostEntry is one key line of a known_hosts file.
type hostEntry struct {
	line    int
	revoked bool
	hosts   []string
	key     gossh.PublicKey
}

type knownHosts struct {
	fs      afero.Fs
	file    string
	entries []hostEntry
}

func loadKnownHosts(fs afero.Fs, file string) (*knownHosts, error) {
	kh := &knownHosts{fs: fs, file: file}
	data, err := afero.ReadFile(fs, file)
	if errors.Is(err, iofs.ErrNotExist) {
		return kh, nil
	}
	if err != nil {
		return nil, err
	}
	for i, raw := range bytes.Split(data, []byte("\n")) {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 || line[0] == '#' {
			continue
		}
		marker, hosts, key, _, _, err := gossh.ParseKnownHosts(line)
		if err != nil {
			continue // OpenSSH skips what it cannot parse, too
		}
		if marker == "cert-authority" {
			continue
		}
		kh.entries = append(kh.entries, hostEntry{line: i + 1, revoked: marker == "revoked", hosts: hosts, key: key})
	}
	return kh, nil
}

// lookup returns the entries that name host, where host is
// knownhosts.Normalize'd: "name" on port 22, "[name]:port" otherwise.
func (kh *knownHosts) lookup(host string) []hostEntry {
	var out []hostEntry
	for _, e := range kh.entries {
		if hostsMatch(e.hosts, host) {
			out = append(out, e)
		}
	}
	return out
}

// hostsMatch applies one entry's comma-separated patterns: plain names,
// * and ? wildcards, !negations and |1|salt|hash hashed names.
func hostsMatch(patterns []string, host string) bool {
	matched := false
	for _, p := range patterns {
		neg := strings.HasPrefix(p, "!")
		p = strings.TrimPrefix(p, "!")
		if !patternMatch(p, host) {
			continue
		}
		if neg {
			return false
		}
		matched = true
	}
	return matched
}

func patternMatch(p, host string) bool {
	if strings.HasPrefix(p, "|1|") {
		salt64, hash64, ok := strings.Cut(p[3:], "|")
		if !ok {
			return false
		}
		salt, err1 := base64.StdEncoding.DecodeString(salt64)
		want, err2 := base64.StdEncoding.DecodeString(hash64)
		if err1 != nil || err2 != nil {
			return false
		}
		mac := hmac.New(sha1.New, salt)
		mac.Write([]byte(host)) //nolint:errcheck,gosec // a hash cannot fail to write
		return hmac.Equal(mac.Sum(nil), want)
	}
	if !strings.ContainsAny(p, "*?") {
		return strings.EqualFold(p, host)
	}
	return globMatch(strings.ToLower(p), strings.ToLower(host))
}

// globMatch is * and ? only. path.Match would read the brackets of
// "[host]:port" as a character class.
func globMatch(p, s string) bool {
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for i := len(s); i >= 0; i-- {
				if globMatch(p[1:], s[i:]) {
					return true
				}
			}
			return false
		case '?':
			if s == "" {
				return false
			}
			p, s = p[1:], s[1:]
		default:
			if s == "" || p[0] != s[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return s == ""
}

// algorithms are the host key algorithms to ask for, so that a server with
// several keys presents the one we already know.
func (kh *knownHosts) algorithms(host string) []string {
	var algos []string
	seen := map[string]bool{}
	add := func(a string) {
		if !seen[a] {
			seen[a] = true
			algos = append(algos, a)
		}
	}
	for _, e := range kh.lookup(host) {
		if e.revoked {
			continue
		}
		if t := e.key.Type(); t == gossh.KeyAlgoRSA {
			add(gossh.KeyAlgoRSASHA512)
			add(gossh.KeyAlgoRSASHA256)
			add(gossh.KeyAlgoRSA)
		} else {
			add(t)
		}
	}
	return algos
}

// add appends key for host, creating the file and its directory.
func (kh *knownHosts) add(host string, key gossh.PublicKey) error {
	if err := kh.fs.MkdirAll(path.Dir(kh.file), 0o700); err != nil {
		return err
	}
	data, err := afero.ReadFile(kh.fs, kh.file)
	if err != nil && !errors.Is(err, iofs.ErrNotExist) {
		return err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		data = append(data, '\n')
	}
	data = append(data, knownhosts.Line([]string{host}, key)+"\n"...)
	return afero.WriteFile(kh.fs, kh.file, data, 0o600)
}

// keyName is how OpenSSH names a key type in its messages: ED25519, RSA, ...
func keyName(k gossh.PublicKey) string {
	t := strings.TrimPrefix(k.Type(), "ssh-")
	if strings.HasPrefix(t, "ecdsa") {
		return "ECDSA"
	}
	return strings.ToUpper(t)
}

// hostKeyCheck is the HostKeyCallback: accept a known key, ask about an
// unknown host, refuse a changed key.
func (kh *knownHosts) hostKeyCheck(t *tty, display string) gossh.HostKeyCallback {
	return func(hostport string, _ net.Addr, key gossh.PublicKey) error {
		host := knownhosts.Normalize(hostport)
		entries := kh.lookup(host)
		for _, e := range entries {
			if e.revoked && bytes.Equal(e.key.Marshal(), key.Marshal()) {
				t.print(fmt.Sprintf("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n"+
					"@       WARNING: REVOKED HOST KEY DETECTED!               @\n"+
					"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n"+
					"The %s host key for %s is marked as revoked (%s:%d).\n",
					keyName(key), host, kh.file, e.line))
				return errors.New("host key for " + host + " is revoked")
			}
		}
		var offending []hostEntry
		for _, e := range entries {
			if e.revoked {
				continue
			}
			if bytes.Equal(e.key.Marshal(), key.Marshal()) {
				return nil
			}
			offending = append(offending, e)
		}
		fp := gossh.FingerprintSHA256(key)
		if len(offending) > 0 {
			var where strings.Builder
			for _, e := range offending {
				fmt.Fprintf(&where, "Offending %s key in %s:%d\n", keyName(e.key), kh.file, e.line)
			}
			t.print("@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n" +
				"@    WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!     @\n" +
				"@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@@\n" +
				"IT IS POSSIBLE THAT SOMEONE IS DOING SOMETHING NASTY!\n" +
				"Someone could be eavesdropping on you right now (man-in-the-middle attack)!\n" +
				"It is also possible that a host key has just been changed.\n" +
				"The fingerprint for the " + keyName(key) + " key sent by the remote host is\n" +
				fp + ".\n" +
				"Please contact your system administrator.\n" +
				"Add correct host key in " + kh.file + " to get rid of this message.\n" +
				where.String() +
				"Host key for " + display + " has changed and you have requested strict checking.\n")
			return errors.New("host key verification failed")
		}
		t.print("The authenticity of host '" + display + "' can't be established.\n" +
			keyName(key) + " key fingerprint is " + fp + ".\n" +
			"This key is not known by any other names.\n")
		ok, err := t.yesNo("Are you sure you want to continue connecting (yes/no)? ")
		if err != nil {
			return err
		}
		if !ok {
			return errors.New("host key verification failed")
		}
		if err := kh.add(host, key); err != nil {
			t.print("Failed to add the host to the list of known hosts (" + kh.file + "): " + err.Error() + "\n")
		} else {
			t.print("Warning: Permanently added '" + host + "' (" + keyName(key) + ") to the list of known hosts.\n")
		}
		kh.entries = append(kh.entries, hostEntry{hosts: []string{host}, key: key})
		return nil
	}
}

// displayHost is how OpenSSH names the host in its prompts.
func displayHost(host string, port uint16) string {
	if port == 22 {
		return host
	}
	return "[" + host + "]:" + strconv.Itoa(int(port))
}
