package shell

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestBackgroundJobOutlivesItsLine runs a line the way the web session does,
// canceling the line's context once Run returns. A job started with & must
// keep running past that, which needs an interactive runner.
func TestBackgroundJobOutlivesItsLine(t *testing.T) {
	var out syncBuilder
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := sh.Run(ctx, "{ sleep 0.3; echo done-late; } &"); err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(out.String(), "done-late") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("background job died with its line; output %q", out.String())
}

// syncBuilder is a strings.Builder safe for a background job to write to
// while the test reads it.
type syncBuilder struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuilder) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuilder) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
