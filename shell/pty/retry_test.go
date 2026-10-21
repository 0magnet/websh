package pty

import (
	"testing"
	"time"
)

func TestRetryWait(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, maxRetryWait, maxRetryWait}
	for n, w := range want {
		if got := retryWait(n); got != w {
			t.Errorf("retryWait(%d) = %v, want %v", n, got, w)
		}
	}
	if got := retryWait(1000); got != maxRetryWait {
		t.Errorf("retryWait(1000) = %v, want %v", got, maxRetryWait)
	}
}
