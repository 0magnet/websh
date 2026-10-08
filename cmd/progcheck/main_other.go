//go:build !js

package main

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

func main() {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "progcheck: no terminal:", err)
		os.Exit(1)
	}
	defer tty.Close()                       //nolint:errcheck // the end of the program
	old, err := term.MakeRaw(int(tty.Fd())) //nolint:gosec // a file descriptor fits an int
	if err != nil {
		fmt.Fprintln(os.Stderr, "progcheck:", err)
		os.Exit(1)
	}
	res, err := run(tty, tty, 3*time.Second)
	term.Restore(int(tty.Fd()), old) //nolint:errcheck,gosec // back as it was, as far as it goes
	if err != nil {
		fmt.Fprintln(os.Stderr, "progcheck:", err)
		os.Exit(1)
	}
	report(os.Stdout, res)
}
