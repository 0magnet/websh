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
// 1.26 it compiles, but the websh build goes from about 3 minutes to about 37:
// charmbracelet/x/ansi imports go-runewidth, whose init writes into a
// [2][0x110000]byte table, and stock TinyGo's interp serializes such a global
// in time quadratic in its size. 0magnet/tinygo e5f7d0a9 fixes that (0.18s for
// the same table); the CI build uses stock TinyGo. Measured 2026-10-03/04.
func Register() {
	shell.RegisterApplet("mosh", "mobile shell (in the standard Go build)",
		func(_ context.Context, _ *shell.Shell, hc *interp.HandlerContext, _ []string) int {
			shell.Println(hc.Stderr, "mosh: not in the TinyGo build of websh; open the standard Go build")
			shell.Println(hc.Stderr, "      (the go/ path beside this page)")
			return 1
		})
}
