//go:build unix

package console

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
)

// The console is driven as a person drives it: on a terminal of its own, which
// is its controlling terminal, so that Ctrl+C is the kernel's SIGINT and not a
// byte. That takes a process, so the test binary runs itself as one.
func TestMain(m *testing.M) {
	if os.Getenv("WEBSH_CONSOLE_CHILD") == "1" {
		code, err := Run(Options{Host: "test"})
		if err != nil {
			os.Stderr.WriteString(err.Error() + "\n") //nolint:errcheck,gosec // the test reads the exit status
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

// session is a console in a child process, on a pty.
type session struct {
	t    *testing.T
	cmd  *exec.Cmd
	tty  *os.File
	mu   sync.Mutex
	out  bytes.Buffer
	read int // how much of out the last expect consumed
	done chan struct{}
}

func start(t *testing.T) *session {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$") //nolint:gosec // this test binary, run as the console
	cmd.Env = append(os.Environ(), "WEBSH_CONSOLE_CHILD=1", "TERM=xterm-256color")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 24, Cols: 80})
	if err != nil {
		t.Fatal(err)
	}
	s := &session{t: t, cmd: cmd, tty: tty, done: make(chan struct{})}
	go func() {
		defer close(s.done)
		buf := make([]byte, 4096)
		for {
			n, err := tty.Read(buf)
			s.mu.Lock()
			s.out.Write(buf[:n])
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		cmd.Process.Kill() //nolint:errcheck,gosec // it may have exited
		cmd.Wait()         //nolint:errcheck,gosec // reaped; the status was the test's business
		tty.Close()        //nolint:errcheck,gosec // the pty is done
	})
	s.expect("$ ")
	return s
}

func (s *session) send(keys string) {
	s.t.Helper()
	if _, err := io.WriteString(s.tty, keys); err != nil {
		s.t.Fatal(err)
	}
}

// expect waits for want to appear in the output after what the last expect
// consumed.
func (s *session) expect(want string) {
	s.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		got := s.out.String()[s.read:]
		if i := strings.Index(got, want); i >= 0 {
			s.read += i + len(want)
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.t.Fatalf("no %q in output:\n%q", want, s.out.String()[s.read:])
}

// exit waits for the console to exit and returns its status.
func (s *session) exit() int {
	s.t.Helper()
	errc := make(chan error, 1)
	go func() { errc <- s.cmd.Wait() }()
	select {
	case err := <-errc:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		if err != nil {
			s.t.Fatal(err)
		}
		return 0
	case <-time.After(5 * time.Second):
		s.t.Fatal("the console did not exit")
		return -1
	}
}

// Output is checked for what only running the line could print: the typed
// line is echoed too, so it is written to compute what it prints.

func TestRunsALine(t *testing.T) {
	s := start(t)
	s.send("echo hello-$((1+2))\r")
	s.expect("hello-3")
	s.expect("test\x1b[0m:") // the prompt again, with the host's name
}

func TestAProgramGetsTheTerminal(t *testing.T) {
	s := start(t)
	s.send("sh -c '[ -t 0 ] && echo stdin-is-$((6*7))'\r")
	s.expect("stdin-is-42")
}

func TestCtrlCStopsAProgram(t *testing.T) {
	s := start(t)
	s.send("sleep 30\r")
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	s.send("\x03")
	s.expect("$ ")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the prompt took %v to come back", d)
	}
	s.send("echo after-$((1+1))\r")
	s.expect("after-2")
}

// An applet reading the terminal is woken by Ctrl+C, and does not keep the
// next line's input: the line typed after it runs.
func TestCtrlCEndsARead(t *testing.T) {
	s := start(t)
	s.send("read x; echo read-ended\r")
	time.Sleep(300 * time.Millisecond)
	s.send("\x03")
	s.expect("$ ")
	s.send("echo next-$((2+2))\r")
	s.expect("next-4")
}

func TestHistory(t *testing.T) {
	s := start(t)
	s.send("echo one-$((0+1))\r")
	s.expect("one-1")
	s.send("\x1b[A\r")
	s.expect("one-1")
}

func TestExitStatus(t *testing.T) {
	s := start(t)
	s.send("exit 7\r")
	if code := s.exit(); code != 7 {
		t.Fatalf("exit status %d, want 7", code)
	}
}

func TestCtrlDExits(t *testing.T) {
	s := start(t)
	s.send("\x04")
	if code := s.exit(); code != 0 {
		t.Fatalf("exit status %d, want 0", code)
	}
}

// The terminal is left as the shell found it: cooked, with echo.
func TestTerminalRestoredAfterAProgramGoesRaw(t *testing.T) {
	s := start(t)
	s.send("stty raw -echo\r")
	s.expect("$ ")
	s.send("stty -a | tr ' ;' '\\n\\n' | grep -x -- '-\\?echo' | sed 's/^/flag:/'\r")
	s.expect("flag:echo")
}

// A full-screen applet takes the terminal raw and gives it back: edit quits on
// Ctrl+Q, which a cooked terminal keeps for flow control.
func TestAFullScreenApplet(t *testing.T) {
	s := start(t)
	s.send("edit " + t.TempDir() + "/f.txt\r")
	time.Sleep(300 * time.Millisecond)
	s.send("\x11")
	s.expect("$ ")
	s.send("echo back-$((4+4))\r")
	s.expect("back-8")
}

// A console in this process, on a pty that is not the process's terminal,
// is interrupted through Options.Signals, as a window's tab is.
func TestSignalsInProcess(t *testing.T) {
	ptm, pts, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer ptm.Close() //nolint:errcheck
	sigs := make(chan os.Signal, 1)
	done := make(chan int, 1)
	go func() {
		code, _ := Run(Options{Host: "test", In: pts, Out: pts, Signals: sigs}) //nolint:errcheck
		pts.Close()                                                             //nolint:errcheck,gosec
		done <- code
	}()
	var mu sync.Mutex
	var out bytes.Buffer
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptm.Read(buf)
			mu.Lock()
			out.Write(buf[:n])
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	wait := func(want string) {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			mu.Lock()
			ok := strings.Contains(out.String(), want)
			mu.Unlock()
			if ok {
				return
			}
		}
		mu.Lock()
		defer mu.Unlock()
		t.Fatalf("no %q in output:\n%q", want, out.String())
	}
	wait("$ ")
	io.WriteString(ptm, "sleep 30; echo after-$((1+1))\r") //nolint:errcheck,gosec
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	sigs <- os.Interrupt
	io.WriteString(ptm, "echo next-$((2+2))\r") //nolint:errcheck,gosec
	wait("next-4")
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("the interrupt took %v", d)
	}
	mu.Lock()
	if strings.Contains(out.String(), "after-2") {
		t.Error("the interrupted line went on to its next command")
	}
	mu.Unlock()
	io.WriteString(ptm, "exit 3\r") //nolint:errcheck,gosec
	select {
	case code := <-done:
		if code != 3 {
			t.Fatalf("exit status %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the console did not exit")
	}
}
