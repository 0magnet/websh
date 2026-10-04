package ssh

import (
	"errors"
	"io"
	"strings"
)

// errInterrupted is Ctrl-C at a prompt.
var errInterrupted = errors.New("interrupted")

// tty is the terminal the prompts talk to. Input is raw: no echo, Enter is a
// carriage return, and erasing is up to us.
type tty struct {
	in  io.Reader
	out io.Writer
}

func (t *tty) print(s string) {
	io.WriteString(t.out, strings.ReplaceAll(s, "\n", "\r\n")) //nolint:errcheck,gosec // nowhere to report a terminal write error
}

// readLine prompts and reads one line, a byte at a time so that nothing past
// the Enter is taken from the reader — what follows belongs to the session.
func (t *tty) readLine(prompt string, echo bool) (string, error) {
	t.print(prompt)
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := t.in.Read(b)
		if n == 0 {
			if err != nil {
				t.print("\n")
				if errors.Is(err, io.EOF) && len(line) > 0 {
					return string(line), nil
				}
				return "", err
			}
			continue
		}
		switch c := b[0]; c {
		case '\r', '\n':
			t.print("\n")
			return string(line), nil
		case 0x03:
			t.print("^C\n")
			return "", errInterrupted
		case 0x04:
			if len(line) == 0 {
				t.print("\n")
				return "", io.EOF
			}
		case 0x7f, '\b':
			if len(line) > 0 {
				line = line[:len(line)-1]
				if echo {
					t.print("\b \b")
				}
			}
		case 0x15: // Ctrl-U
			if echo {
				t.print(strings.Repeat("\b \b", len(line)))
			}
			line = line[:0]
		default:
			if c < 0x20 {
				continue
			}
			line = append(line, c)
			if echo {
				t.out.Write(b) //nolint:errcheck,gosec // nowhere to report a terminal write error
			}
		}
	}
}

// password reads a secret without echoing it.
func (t *tty) password(prompt string) (string, error) { return t.readLine(prompt, false) }

// yesNo asks the way OpenSSH does: only "yes" or "no" will do.
func (t *tty) yesNo(prompt string) (bool, error) {
	for {
		ans, err := t.readLine(prompt, true)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "yes":
			return true, nil
		case "no":
			return false, nil
		}
		prompt = "Please type 'yes' or 'no': "
	}
}
