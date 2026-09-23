package shell

import (
	"context"
	"strings"
	"testing"
)

// runWithExec is run() with an embedder's Exec hook installed.
func runWithExec(t *testing.T, hook func(context.Context, []string) (int, bool), lines ...string) string {
	t.Helper()
	var out strings.Builder
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	sh.Exec = hook
	for _, line := range lines {
		if _, err := sh.Run(context.Background(), line); err != nil &&
			!strings.Contains(err.Error(), "exit status") {
			t.Fatalf("run %q: %v", line, err)
		}
	}
	return out.String()
}

func TestTheEmbeddersCommandIsReached(t *testing.T) {
	var saw []string
	got := runWithExec(t, func(_ context.Context, args []string) (int, bool) {
		saw = args
		return 0, true
	}, "rack --monitor 3")
	if strings.Contains(got, "command not found") {
		t.Fatalf("the hook was not consulted: %q", got)
	}
	if strings.Join(saw, " ") != "rack --monitor 3" {
		t.Fatalf("the hook saw %q, want the whole command line", strings.Join(saw, " "))
	}
}

func TestAnUnhandledCommandFallsThroughToNotFound(t *testing.T) {
	// handled=false has to mean "I do not know this", or an embedder would
	// have to reimplement the shell's own error to decline one command.
	got := runWithExec(t, func(context.Context, []string) (int, bool) {
		return 0, false
	}, "nosuchthing")
	if !strings.Contains(got, "command not found") {
		t.Fatalf("an unhandled command gave %q, want command not found", got)
	}
}

func TestAppletsWinANameClash(t *testing.T) {
	// An embedder extends the command set; it does not get to replace echo
	// with something else by accident.
	reached := false
	got := runWithExec(t, func(context.Context, []string) (int, bool) {
		reached = true
		return 0, true
	}, "echo hello")
	if reached {
		t.Fatal("the hook was offered a built-in applet")
	}
	if got != "hello\n" {
		t.Fatalf("echo produced %q", got)
	}
}

func TestTheExitStatusIsTheShellsOwn(t *testing.T) {
	got := runWithExec(t, func(context.Context, []string) (int, bool) {
		return 3, true
	}, "rack", "echo $?")
	if got != "3\n" {
		t.Fatalf("status came back as %q, want 3", got)
	}
}

func TestAnOutOfRangeStatusIsClamped(t *testing.T) {
	// A shell status is one byte. 256 reported raw would read as success,
	// which is the one wrong answer.
	for _, code := range []int{256, -1, 1 << 20} {
		got := runWithExec(t, func(context.Context, []string) (int, bool) {
			return code, true
		}, "rack", "echo $?")
		if got == "0\n" {
			t.Fatalf("a status of %d was reported as success", code)
		}
	}
}

func TestNoHookIsTheOldBehaviour(t *testing.T) {
	if got := run(t, "nosuchthing"); !strings.Contains(got, "command not found") {
		t.Fatalf("with no hook a missing command gave %q", got)
	}
}
