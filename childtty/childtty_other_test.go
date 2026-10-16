//go:build unix

package childtty

import (
	"os"
	"strings"
	"testing"
)

func TestDeviceIsTheHostsTerminal(t *testing.T) {
	defer func() { Terminal = nil }()
	if d := device(); d != "/dev/tty" {
		t.Fatalf("no Terminal: %q", d)
	}
	Terminal = func() *os.File { return nil }
	if d := device(); d != "/dev/tty" {
		t.Fatalf("Terminal returning nil: %q", d)
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close() //nolint:errcheck
	defer w.Close() //nolint:errcheck
	Terminal = func() *os.File { return r }
	if d := device(); !strings.HasPrefix(d, "/dev/fd/") {
		t.Fatalf("Terminal returning a file: %q", d)
	}
	if _, err := os.Stat(device()); err != nil {
		t.Fatal(err)
	}
}
