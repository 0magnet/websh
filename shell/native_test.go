//go:build unix

package shell

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/0magnet/sh/v3/interp"
)

// runOS runs one line in a fresh shell on the OS, and returns what it wrote
// and its exit status.
func runOS(t *testing.T, line string) (string, int) {
	t.Helper()
	var out bytes.Buffer
	s, err := NewOS(nil, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Run(context.Background(), line)
	var es interp.ExitStatus
	switch {
	case err == nil:
		return out.String(), 0
	case errors.As(err, &es):
		return out.String(), int(es)
	}
	t.Fatalf("%s: %v", line, err)
	return "", 0
}

func need(t *testing.T, programs ...string) {
	t.Helper()
	for _, p := range programs {
		if _, err := exec.LookPath(p); err != nil {
			t.Skipf("no %s on this machine", p)
		}
	}
}

// A stand-in applet gives way to the machine's own program: uname is the
// machine's, not the page's.
func TestOSProgramBeatsAStandIn(t *testing.T) {
	need(t, "uname")
	out, _ := runOS(t, "uname -s")
	if strings.Contains(out, "websh") {
		t.Fatalf("uname ran the applet: %q", out)
	}
}

// websh's own applets run even where a program has the name.
func TestOwnAppletsStayFirst(t *testing.T) {
	out, _ := runOS(t, "which edit")
	if out != "edit: websh applet\n" {
		t.Fatalf("which edit = %q", out)
	}
}

func TestWhichNamesTheProgramThatRuns(t *testing.T) {
	need(t, "sh")
	want, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	out, _ := runOS(t, "which sh")
	if strings.TrimSpace(out) != want {
		t.Fatalf("which sh = %q, want %q", out, want)
	}
}

// A path is a program, even where the name is an applet's.
func TestAPathIsAProgram(t *testing.T) {
	need(t, "sh")
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	out, _ := runOS(t, sh+" -c 'echo from-$((2*3))'")
	if out != "from-6\n" {
		t.Fatalf("got %q", out)
	}
}

// A program sees what the shell exported, and not what it unset.
func TestProgramEnvironment(t *testing.T) {
	need(t, "sh")
	out, _ := runOS(t, `export WEBSH_T=yes; unset HOME; sh -c 'echo "$WEBSH_T ${HOME-unset}"'`)
	if out != "yes unset\n" {
		t.Fatalf("got %q", out)
	}
}

func TestOSCommandNotFound(t *testing.T) {
	out, code := runOS(t, "websh-no-such-command")
	if code != 127 || !strings.Contains(out, "websh-no-such-command: command not found") {
		t.Fatalf("got %d %q", code, out)
	}
}

func TestCompleteOffersPrograms(t *testing.T) {
	need(t, "sh")
	s, err := NewOS(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Complete("s", true); !slices.Contains(got, "sh") {
		t.Fatalf("Complete(s) = %v, no sh", got)
	}
}
