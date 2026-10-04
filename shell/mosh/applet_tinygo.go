//go:build tinygo

package mosh

import (
	"context"

	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
)

// Register adds a mosh command that explains why it is missing here.
//
// mosh-go keeps a terminal emulator (github.com/unixshells/vt-go) in its
// client package, and that pulls in charmbracelet/ultraviolet, which needs
// hash/maphash — and TinyGo has no hash/maphash. The standard Go build has
// the real command.
func Register() {
	shell.RegisterApplet("mosh", "mobile shell (standard Go build only)",
		func(_ context.Context, _ *shell.Shell, hc *interp.HandlerContext, _ []string) int {
			shell.Println(hc.Stderr, "mosh: not available in the TinyGo build, which lacks hash/maphash;")
			shell.Println(hc.Stderr, "      open the standard Go build of this page (the go/ path beside it)")
			return 1
		})
}
