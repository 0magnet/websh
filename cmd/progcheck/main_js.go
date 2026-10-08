//go:build js && wasm

package main

import (
	"fmt"
	"os"
	"time"

	"github.com/0magnet/bottle/proc"
)

func main() {
	t, ok := proc.Term()
	if !ok {
		fmt.Fprintln(os.Stderr, "progcheck: no terminal to ask (run it with its output on the terminal)")
		os.Exit(1)
	}
	t.SetRaw(true)
	res, err := run(t, t, 3*time.Second)
	t.SetRaw(false)
	if err != nil {
		fmt.Fprintln(os.Stderr, "progcheck:", err)
		os.Exit(1)
	}
	report(t, res)
	os.Exit(0) // TinyGo keeps a js program alive after main returns
}
