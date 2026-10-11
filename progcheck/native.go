//go:build !js

package progcheck

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/term"
)

// Main asks the terminal on /dev/tty and prints the report.
func Main() error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("no terminal: %w", err)
	}
	defer tty.Close()                       //nolint:errcheck // the end of the program
	old, err := term.MakeRaw(int(tty.Fd())) //nolint:gosec // a file descriptor fits an int
	if err != nil {
		return err
	}
	res, err := run(tty, tty, 3*time.Second)
	term.Restore(int(tty.Fd()), old) //nolint:errcheck,gosec // back as it was, as far as it goes
	if err != nil {
		return err
	}
	report(os.Stdout, res)
	return nil
}
