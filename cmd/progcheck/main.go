//go:build js && wasm

// Command progcheck is the terminal check (package progcheck) as a js/wasm
// program, run from websh's filesystem. On a desktop it is `websh progcheck`.
package main

import (
	"fmt"
	"os"

	"github.com/0magnet/websh/progcheck"
)

func main() {
	if err := progcheck.Main(); err != nil {
		fmt.Fprintln(os.Stderr, "progcheck:", err)
		os.Exit(1)
	}
	os.Exit(0) // TinyGo keeps a js program alive after main returns
}
