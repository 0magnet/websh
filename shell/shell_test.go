package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

// run executes one or more lines in a fresh shell and returns stdout.
func run(t *testing.T, lines ...string) string {
	t.Helper()
	var out strings.Builder
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, line := range lines {
		if _, err := sh.Run(ctx, line); err != nil {
			// exit-status errors are part of normal shell life; only
			// fail the test on other errors
			if !strings.Contains(err.Error(), "exit status") {
				t.Fatalf("run %q: %v", line, err)
			}
		}
	}
	return out.String()
}

func TestEcho(t *testing.T) {
	if got := run(t, "echo hello world"); got != "hello world\n" {
		t.Fatalf("got %q", got)
	}
}

func TestRedirectAndCat(t *testing.T) {
	got := run(t, "echo data > f.txt", "cat f.txt")
	if got != "data\n" {
		t.Fatalf("got %q", got)
	}
	// append
	got = run(t, "echo one > f", "echo two >> f", "cat f")
	if got != "one\ntwo\n" {
		t.Fatalf("append got %q", got)
	}
}

func TestPipes(t *testing.T) {
	got := run(t, "seq 10 | grep 1")
	if got != "1\n10\n" {
		t.Fatalf("got %q", got)
	}
	got = run(t, "seq 3 | sort -r")
	if got != "3\n2\n1\n" {
		t.Fatalf("sort got %q", got)
	}
	got = run(t, "printf 'b\\na\\nb\\n' | sort | uniq -c | sort -rn | head -n 1")
	if !strings.Contains(got, "2 b") {
		t.Fatalf("pipeline got %q", got)
	}
}

func TestControlFlow(t *testing.T) {
	got := run(t, "for i in 1 2 3; do echo line$i; done")
	if got != "line1\nline2\nline3\n" {
		t.Fatalf("got %q", got)
	}
	got = run(t, "if [ -f /etc/motd ]; then echo yes; else echo no; fi")
	if got != "yes\n" {
		t.Fatalf("if got %q", got)
	}
	got = run(t, "x=5", "while [ $x -gt 3 ]; do echo $x; x=$((x-1)); done")
	if got != "5\n4\n" {
		t.Fatalf("while got %q", got)
	}
}

func TestMultilineContinuation(t *testing.T) {
	var out strings.Builder
	sh, _ := New(nil, strings.NewReader(""), &out, &out)
	ctx := context.Background()
	more, err := sh.Run(ctx, "for i in a b; do")
	if err != nil || !more {
		t.Fatalf("expected continuation, got more=%v err=%v", more, err)
	}
	more, err = sh.Run(ctx, "echo x$i")
	if err != nil || !more {
		t.Fatalf("expected continuation 2, got more=%v err=%v", more, err)
	}
	more, err = sh.Run(ctx, "done")
	if err != nil || more {
		t.Fatalf("expected completion, got more=%v err=%v", more, err)
	}
	if out.String() != "xa\nxb\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestGlobbing(t *testing.T) {
	got := run(t, "touch a.txt b.txt c.md", "echo *.txt")
	if got != "a.txt b.txt\n" {
		t.Fatalf("got %q", got)
	}
	got = run(t, "for f in /etc/*; do echo $f; done")
	if got != "/etc/motd\n" {
		t.Fatalf("glob got %q", got)
	}
}

func TestCdPwd(t *testing.T) {
	got := run(t, "cd /tmp", "pwd")
	if got != "/tmp\n" {
		t.Fatalf("got %q", got)
	}
	got = run(t, "mkdir -p a/b/c", "cd a/b", "pwd", "cd ..", "pwd")
	if got != "/home/user/a/b\n/home/user/a\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFileApplets(t *testing.T) {
	got := run(t,
		"mkdir d", "echo hi > d/x", "cp -r d d2", "cat d2/x",
		"mv d2/x d2/y", "cat d2/y",
		"rm -r d2", "ls",
	)
	want := "hi\nhi\nd/\ndemo.sh\nreadme.md\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLsLong(t *testing.T) {
	got := run(t, "ls -l /etc")
	if !strings.Contains(got, "motd") || !strings.Contains(got, "-rw-") {
		t.Fatalf("got %q", got)
	}
}

func TestVarsAndSubstitution(t *testing.T) {
	got := run(t, "x=world", `echo "hello $x"`, "echo $((2 + 3))", "echo $(echo nested)")
	if got != "hello world\n5\nnested\n" {
		t.Fatalf("got %q", got)
	}
}

func TestFunctions(t *testing.T) {
	got := run(t, "greet() { echo hi $1; }", "greet bob")
	if got != "hi bob\n" {
		t.Fatalf("got %q", got)
	}
}

func TestSource(t *testing.T) {
	got := run(t, "source demo.sh")
	if !strings.Contains(got, "hello from a shell script!") ||
		!strings.Contains(got, "found: /etc/motd") ||
		!strings.Contains(got, "the answer is 42") {
		t.Fatalf("got %q", got)
	}
}

func TestCommandNotFound(t *testing.T) {
	got := run(t, "nosuchcmd 2>&1; echo status=$?")
	if !strings.Contains(got, "command not found") || !strings.Contains(got, "status=127") {
		t.Fatalf("got %q", got)
	}
}

func TestExitStatusFlow(t *testing.T) {
	got := run(t, "true && echo yes", "false || echo fallback", "grep nope readme.md || echo missed")
	if got != "yes\nfallback\nmissed\n" {
		t.Fatalf("got %q", got)
	}
}

func TestWcGrep(t *testing.T) {
	got := run(t, "seq 100 | wc -l")
	if strings.TrimSpace(got) != "100" {
		t.Fatalf("wc got %q", got)
	}
	got = run(t, "grep -n 'shell script' demo.sh")
	if !strings.HasPrefix(got, "1:") {
		t.Fatalf("grep -n got %q", got)
	}
	got = run(t, "grep -c nothinghere demo.sh; echo rc=$?")
	if !strings.Contains(got, "rc=1") {
		t.Fatalf("grep miss got %q", got)
	}
}

func TestTree(t *testing.T) {
	got := run(t, "mkdir -p p/q", "touch p/f p/q/g", "tree p")
	want := "p\n├── f\n└── q\n    └── g\n"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestCancelSleep(t *testing.T) {
	var out strings.Builder
	sh, _ := New(nil, strings.NewReader(""), &out, &out)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := sh.Run(ctx, "sleep 30")
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
		// returned promptly after cancel
	case <-time.After(2 * time.Second):
		t.Fatal("sleep did not cancel")
	}
}

func TestHeredoc(t *testing.T) {
	var out strings.Builder
	sh, _ := New(nil, strings.NewReader(""), &out, &out)
	ctx := context.Background()
	more, _ := sh.Run(ctx, "cat << EOF")
	if !more {
		t.Fatal("expected heredoc continuation")
	}
	sh.Run(ctx, "line one")
	more, err := sh.Run(ctx, "EOF")
	if more || err != nil {
		t.Fatalf("heredoc end: more=%v err=%v", more, err)
	}
	if out.String() != "line one\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestBinPopulated(t *testing.T) {
	got := run(t, "ls /bin | wc -l")
	if strings.TrimSpace(got) == "0" {
		t.Fatalf("/bin is empty")
	}
	got = run(t, "ls /bin | grep -c grep")
	if strings.TrimSpace(got) != "1" {
		t.Fatalf("grep not in /bin: %q", got)
	}
	got = run(t, "cat /bin/tree")
	if !strings.Contains(got, "directory tree") {
		t.Fatalf("stub content: %q", got)
	}
}
