package ssh

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	gossh "golang.org/x/crypto/ssh"
)

// Terminal is the local side of a session.
type Terminal struct {
	// In is read on the calling goroutine only, and Out takes the remote
	// output. For an interactive session In is raw keys.
	In  io.Reader
	Out io.Writer
	// Err takes a command's stderr; nil means Out.
	Err io.Writer
	// Term is $TERM for the remote pty, and Size the terminal size, polled
	// so a resize reaches the server.
	Term string
	Size func() (cols, rows int)
	// Wake makes a Read blocked on In return, with no data. Without it a
	// session that ends on its own is noticed only at the next key.
	Wake func()
}

// resizePoll is how often the terminal size is checked.
const resizePoll = 250 * time.Millisecond

// lockedWriter serializes the remote stdout and stderr copiers.
type lockedWriter struct {
	mu *sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(b)
}

// Shell runs an interactive login shell on a pty until it exits or the user
// types ~. after a newline. It returns the remote exit status.
func (c *Client) Shell(ctx context.Context, t Terminal) (int, error) {
	sess, err := c.NewSession()
	if err != nil {
		return 255, err
	}
	defer sess.Close() //nolint:errcheck // the session is over either way

	cols, rows := 80, 24
	if t.Size != nil {
		cols, rows = t.Size()
	}
	term := t.Term
	if term == "" {
		term = "xterm-256color"
	}
	modes := gossh.TerminalModes{gossh.ECHO: 1, gossh.TTY_OP_ISPEED: 38400, gossh.TTY_OP_OSPEED: 38400}
	if err := sess.RequestPty(term, rows, cols, modes); err != nil {
		return 255, err
	}
	out := &lockedWriter{mu: new(sync.Mutex), w: t.Out}
	sess.Stdout, sess.Stderr = out, out
	stdin, err := sess.StdinPipe()
	if err != nil {
		return 255, err
	}
	if err := sess.Shell(); err != nil {
		return 255, err
	}

	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = sess.Wait()
		close(done)
		if t.Wake != nil {
			t.Wake()
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			c.Close() //nolint:errcheck,gosec // ends the session
		case <-done:
		}
	}()
	if t.Size != nil {
		go func() {
			tk := time.NewTicker(resizePoll)
			defer tk.Stop()
			for {
				select {
				case <-done:
					return
				case <-tk.C:
				}
				if c2, r2 := t.Size(); c2 != cols || r2 != rows {
					cols, rows = c2, r2
					sess.WindowChange(rows, cols) //nolint:errcheck,gosec // best effort, like ssh
				}
			}
		}()
	}

	quit := forwardKeys(t.In, stdin, done)
	if quit {
		c.Close() //nolint:errcheck,gosec // ~. ends the connection
	}
	<-done
	if quit {
		return 255, nil
	}
	return exitStatus(waitErr)
}

// forwardKeys copies raw keys to the session until it ends, input ends, or
// the user types the escape ~. at the start of a line (true). ~~ sends one ~.
func forwardKeys(in io.Reader, stdin io.WriteCloser, done <-chan struct{}) (quit bool) {
	buf := make([]byte, 4096)
	lineStart, tilde := true, false
	for {
		select {
		case <-done:
			return false
		default:
		}
		n, err := in.Read(buf)
		select {
		case <-done:
			return false
		default:
		}
		var send []byte
		for _, b := range buf[:n] {
			if tilde {
				tilde = false
				switch b {
				case '.':
					if len(send) > 0 {
						stdin.Write(send) //nolint:errcheck,gosec // the connection is going
					}
					return true
				case '~':
					send = append(send, '~')
					lineStart = false
					continue
				default:
					send = append(send, '~')
				}
			}
			if lineStart && b == '~' {
				tilde = true
				continue
			}
			send = append(send, b)
			lineStart = b == '\r' || b == '\n'
		}
		if len(send) > 0 {
			if _, werr := stdin.Write(send); werr != nil {
				return false
			}
		}
		if err != nil {
			stdin.Close() //nolint:errcheck,gosec // EOF to the remote
			return false
		}
	}
}

// Run runs command without a pty, with In as its stdin unless noStdin, and
// returns its exit status.
func (c *Client) Run(ctx context.Context, command string, t Terminal, noStdin bool) (int, error) {
	sess, err := c.NewSession()
	if err != nil {
		return 255, err
	}
	defer sess.Close() //nolint:errcheck // the session is over either way

	// One lock for both: they are usually the same terminal.
	mu := new(sync.Mutex)
	sess.Stdout = &lockedWriter{mu: mu, w: t.Out}
	sess.Stderr = &lockedWriter{mu: mu, w: t.Err}
	if t.Err == nil {
		sess.Stderr = sess.Stdout
	}
	var stdin io.WriteCloser
	if !noStdin {
		if stdin, err = sess.StdinPipe(); err != nil {
			return 255, err
		}
	}
	if err := sess.Start(command); err != nil {
		return 255, err
	}

	done := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = sess.Wait()
		close(done)
		if t.Wake != nil && stdin != nil {
			t.Wake()
		}
	}()
	var canceled atomic.Bool
	go func() {
		select {
		case <-ctx.Done():
			canceled.Store(true)
			c.Close() //nolint:errcheck,gosec // ends the command
		case <-done:
		}
	}()
	if stdin != nil {
		copyStdin(t.In, stdin, done)
	}
	<-done
	if canceled.Load() {
		return 130, nil
	}
	return exitStatus(waitErr)
}

// copyStdin is forwardKeys without the escape: bytes as they come.
func copyStdin(in io.Reader, stdin io.WriteCloser, done <-chan struct{}) {
	buf := make([]byte, 32*1024)
	for {
		select {
		case <-done:
			return
		default:
		}
		n, err := in.Read(buf)
		select {
		case <-done:
			return
		default:
		}
		if n > 0 {
			if _, werr := stdin.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			stdin.Close() //nolint:errcheck,gosec // EOF to the remote
			return
		}
	}
}

// exitStatus maps a Wait error onto an exit status the way ssh does.
func exitStatus(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var ee *gossh.ExitError
	if errors.As(err, &ee) {
		if ee.Signal() != "" && ee.ExitStatus() == 0 {
			return 255, errors.New("remote command killed by signal " + ee.Signal())
		}
		return ee.ExitStatus(), nil
	}
	var missing *gossh.ExitMissingError
	if errors.As(err, &missing) {
		return 255, nil
	}
	return 255, err
}
