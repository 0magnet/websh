//go:build tinygo

package mosh

import (
	"context"

	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
)

// Register adds a mosh command that points at the standard Go build.
//
// mosh-go's terminal model (github.com/unixshells/vt-go) pulls in
// charmbracelet/ultraviolet and with it hash/maphash. TinyGo 0.42 cannot
// compile that against Go 1.27 (it reaches internal/runtime/maps). Against Go
// 1.26 it compiles and works, but the websh build goes from about 3 minutes to
// about 37, single-threaded, and the wasm from 3.7 to 4.6 MB — too much for a
// demo rebuilt on every push. Measured 2026-10-03.
func Register() {
	shell.RegisterApplet("mosh", "mobile shell (in the standard Go build)",
		func(_ context.Context, _ *shell.Shell, hc *interp.HandlerContext, _ []string) int {
			shell.Println(hc.Stderr, "mosh: not in the TinyGo build of websh; open the standard Go build")
			shell.Println(hc.Stderr, "      (the go/ path beside this page)")
			return 1
		})
}
