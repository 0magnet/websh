package shell

import (
	"context"

	"github.com/0magnet/afero"
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
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
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
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
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
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	more, err := sh.Run(ctx, "cat << EOF")
	if err != nil {
		t.Fatal(err)
	}
	if !more {
		t.Fatal("expected heredoc continuation")
	}
	if _, err := sh.Run(ctx, "line one"); err != nil {
		t.Fatal(err)
	}
	more, err = sh.Run(ctx, "EOF")
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

func TestTextTools(t *testing.T) {
	got := run(t, "printf 'a:b:c\\n' | cut -d: -f2")
	if got != "b\n" {
		t.Fatalf("cut = %q", got)
	}
	got = run(t, "echo hello | tr a-z A-Z")
	if got != "HELLO\n" {
		t.Fatalf("tr = %q", got)
	}
	got = run(t, "echo hello | tr -d l")
	if got != "heo\n" {
		t.Fatalf("tr -d = %q", got)
	}
	got = run(t, "echo foo bar | sed s/bar/baz/")
	if got != "foo baz\n" {
		t.Fatalf("sed = %q", got)
	}
	got = run(t, "printf 'aa aa\\n' | sed s/a/b/g")
	if got != "bb bb\n" {
		t.Fatalf("sed g = %q", got)
	}
	got = run(t, "seq 3 | tac")
	if got != "3\n2\n1\n" {
		t.Fatalf("tac = %q", got)
	}
	got = run(t, "printf 'x\\ny\\n' | nl | cut -c6-")
	if !strings.Contains(got, "1\tx") {
		t.Fatalf("nl = %q", got)
	}
}

func TestFindXargsHash(t *testing.T) {
	got := run(t, "mkdir -p a/b", "touch a/x.txt a/b/y.txt a/b/z.md", "find a -name '*.txt' | sort")
	if got != "a/b/y.txt\na/x.txt\n" {
		t.Fatalf("find = %q", got)
	}
	got = run(t, "find /etc -type f")
	if got != "/etc/motd\n" {
		t.Fatalf("find type = %q", got)
	}
	got = run(t, "seq 3 | xargs echo nums:")
	if got != "nums: 1 2 3\n" {
		t.Fatalf("xargs echo = %q", got)
	}
	got = run(t, "echo /etc/motd | xargs wc -l")
	if strings.TrimSpace(got) != "1" {
		t.Fatalf("xargs wc = %q", got)
	}
	got = run(t, "printf hello | md5sum")
	if !strings.HasPrefix(got, "5d41402abc4b2a76b9719d911017c592") {
		t.Fatalf("md5 = %q", got)
	}
	got = run(t, "printf hello | sha256sum")
	if !strings.HasPrefix(got, "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824") {
		t.Fatalf("sha256 = %q", got)
	}
	got = run(t, "printf hi | base64", "printf aGk= | base64 -d")
	if got != "aGk=\nhi" {
		t.Fatalf("base64 = %q", got)
	}
	got = run(t, "printf AB | xxd")
	if !strings.Contains(got, "4142") || !strings.Contains(got, "AB") {
		t.Fatalf("xxd = %q", got)
	}
	got = run(t, "du -s /etc | cut -f2")
	if got != "/etc\n" {
		t.Fatalf("du = %q", got)
	}
	got = run(t, "chmod 700 demo.sh", "stat demo.sh")
	if !strings.Contains(got, "rwx------") {
		t.Fatalf("chmod/stat = %q", got)
	}
}

func TestAwk(t *testing.T) {
	got := run(t, "seq 5 | awk '{ sum += $1 } END { print sum }'")
	if got != "15\n" {
		t.Fatalf("awk sum = %q", got)
	}
	got = run(t, "printf 'a:1\\nb:2\\n' | awk -F: '{ print $2, $1 }'")
	if got != "1 a\n2 b\n" {
		t.Fatalf("awk -F = %q", got)
	}
	got = run(t, "awk '/shell/ { print NR }' demo.sh")
	if got != "1\n" {
		t.Fatalf("awk file = %q", got)
	}
	got = run(t, "echo | awk -v x=7 '{ print x * 6 }'")
	if got != "42\n" {
		t.Fatalf("awk -v = %q", got)
	}
}

func TestJq(t *testing.T) {
	got := run(t, `echo '{"name":"websh","tags":["wasm","shell"]}' | jq -r .name`)
	if got != "websh\n" {
		t.Fatalf("jq .name = %q", got)
	}
	got = run(t, `echo '{"a":[1,2,3]}' | jq -c '.a | map(. * 2)'`)
	if got != "[2,4,6]\n" {
		t.Fatalf("jq map = %q", got)
	}
	got = run(t, `echo '[{"x":1},{"x":2}]' | jq '[.[].x] | add'`)
	if got != "3\n" {
		t.Fatalf("jq add = %q", got)
	}
}

func TestTabCompletion(t *testing.T) {
	var echoed strings.Builder
	e := &LineEditor{
		Echo:   func(s string) { echoed.WriteString(s) },
		Redraw: func(content string, back int) {},
		Complete: func(word string, isFirstWord bool) []string {
			if isFirstWord {
				var out []string
				for _, n := range []string{"grep", "gzip", "cat"} {
					if strings.HasPrefix(n, word) {
						out = append(out, n)
					}
				}
				return out
			}
			return []string{"demo.sh"}
		},
	}
	// unique command completes with trailing space
	e.Input("c\t")
	if e.Line() != "cat " {
		t.Fatalf("line = %q", e.Line())
	}
	// ambiguous prefix extends to common prefix only
	e.Reset()
	e.Input("g\t")
	if e.Line() != "g" { // no progress beyond "g" (grep/gzip share only "g")
		t.Fatalf("ambiguous line = %q", e.Line())
	}
	if !strings.Contains(echoed.String(), "grep  gzip") {
		t.Fatalf("candidates not listed: %q", echoed.String())
	}
	// second word completes as a path
	e.Reset()
	e.Input("cat de\t")
	if e.Line() != "cat demo.sh " {
		t.Fatalf("path completion = %q", e.Line())
	}
}

// feedShell runs a shell whose stdin is scripted, for full-screen
// applets like edit and less.
func feedShell(t *testing.T, stdin string, lines ...string) (*Shell, string) {
	t.Helper()
	var out strings.Builder
	sh, err := New(nil, strings.NewReader(stdin), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, line := range lines {
		if _, err := sh.Run(ctx, line); err != nil {
			t.Fatalf("run %q: %v", line, err)
		}
	}
	return sh, out.String()
}

func TestEditCreateAndSave(t *testing.T) {
	// type two lines, Ctrl+S, Ctrl+Q
	stdin := "hello\rworld\x13\x11"
	sh, out := feedShell(t, stdin, "edit note.txt")
	data, err := afero.ReadFile(sh.FS, "/home/user/note.txt")
	if err != nil {
		t.Fatalf("file not saved: %v", err)
	}
	if string(data) != "hello\nworld\n" {
		t.Fatalf("content = %q", string(data))
	}
	if !strings.Contains(out, "\x1b[?1049h") || !strings.Contains(out, "\x1b[?1049l") {
		t.Fatal("alt screen not used")
	}
	if !strings.Contains(out, "saved 2 lines") {
		t.Fatalf("no save message in %q", out[len(out)-200:])
	}
}

func TestEditModify(t *testing.T) {
	// open existing file, go to end of first line, append "!", save+quit
	sh, _ := feedShell(t, "", "echo abc > f.txt", "echo def >> f.txt")
	var out strings.Builder
	sh2, err := New(nil, strings.NewReader("\x1b[F!\x13\x11"), &out, &out)
	_ = sh2
	if err != nil {
		t.Fatal(err)
	}
	// reuse first shell's FS so the file exists
	sh2.FS = sh.FS
	if _, err := sh2.Run(context.Background(), "edit f.txt"); err != nil {
		t.Fatal(err)
	}
	data, err := afero.ReadFile(sh.FS, "/home/user/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc!\ndef\n" {
		t.Fatalf("content = %q", string(data))
	}
}

func TestEditBackspaceDeleteNav(t *testing.T) {
	// "abx<bs>c" -> abc ; then down, home, del removes 'd'
	stdin := "abx\x7fc\r" + "def" + "\x1b[H" + "\x1b[3~" + "\x13\x11"
	sh, _ := feedShell(t, stdin, "edit t.txt")
	data, err := afero.ReadFile(sh.FS, "/home/user/t.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abc\nef\n" {
		t.Fatalf("content = %q", string(data))
	}
}

func TestLessPager(t *testing.T) {
	// stdin script: space (page down), q (quit)
	sh, out := feedShell(t, " q", "seq 100 > big.txt", "less big.txt")
	_ = sh
	if !strings.Contains(out, "\x1b[?1049h") {
		t.Fatal("less did not use alt screen")
	}
	if !strings.Contains(out, "big.txt") {
		t.Fatal("status bar missing")
	}
	// after one page down on 24-line default, line 25 should render
	if !strings.Contains(out, "25") {
		t.Fatalf("page down did not render next page")
	}
}
