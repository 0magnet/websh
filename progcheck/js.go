//go:build js && wasm

package progcheck

import (
	"errors"
	"time"

	"github.com/0magnet/bottle/proc"
)

// Main asks the terminal the program runs in and prints the report.
func Main() error {
	t, ok := proc.Term()
	if !ok {
		return errors.New("no terminal to ask (run it with its output on the terminal)")
	}
	t.SetRaw(true)
	res, err := run(t, t, 3*time.Second)
	t.SetRaw(false)
	if err != nil {
		return err
	}
	report(t, res)
	return nil
}
