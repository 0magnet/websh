package pty

import "time"

// maxRetryWait caps the wait between attempts to reattach a session. The
// attempts do not stop: a server restart outlasts any short budget, and ~. ends
// the session meanwhile.
const maxRetryWait = 10 * time.Second

// retryWait is how long to wait before reconnect attempt n+1: one second,
// doubling to maxRetryWait.
func retryWait(n int) time.Duration {
	if n >= 4 {
		return maxRetryWait
	}
	return min(time.Second<<n, maxRetryWait)
}
