//go:build js && wasm

// Command progdemo is the progressive-terminal demo (package progdemo) as a
// js/wasm program, run from websh's filesystem. On a desktop it is
// `websh progdemo`.
package main

import (
	"fmt"
	"os"

	"github.com/0magnet/websh/progdemo"
)

func main() {
	// Run returns only when the screen cannot start; quitting exits.
	fmt.Fprintln(os.Stderr, "progdemo:", progdemo.Run())
	os.Exit(1)
}
