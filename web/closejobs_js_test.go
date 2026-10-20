//go:build js && wasm

package web

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/0magnet/afero"

	"github.com/0magnet/websh/shell"
)

// Closing a session ends its background jobs. They outlive the line that
// started them, so nothing else would, and a closed tab left them running.
func TestCloseEndsBackgroundJobs(t *testing.T) {
	var out strings.Builder
	sh, err := shell.New(afero.NewMemMapFs(), strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := sh.Run(ctx, "sleep 30 &"); err != nil {
		t.Fatal(err)
	}
	cancel()

	jobs := func() string {
		out.Reset()
		if _, err := sh.Run(context.Background(), "jobs"); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if got := jobs(); !strings.Contains(got, "Running") {
		t.Fatalf("job is not running before Close: %q", got)
	}

	s := &Session{Shell: sh, lines: make(chan string)}
	s.Close()
	deadline := time.Now().Add(5 * time.Second)
	for strings.Contains(jobs(), "Running") {
		if time.Now().After(deadline) {
			t.Fatal("job still running after Close")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
