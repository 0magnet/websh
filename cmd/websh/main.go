//go:build js && wasm

// websh: a bash-like shell running entirely in the browser —
// github.com/0magnet/sh interpreting over an in-memory filesystem,
// displayed by an xterm-go terminal.
//
// Almost nothing is left here. The terminal, the line editor and the command
// loop are web.NewSession; what remains is this program's own concerns —
// restoring the filesystem from IndexedDB, flushing it back after each command,
// and the applets that only make sense in a browser.
package main

import (
	"context"
	"strconv"
	"sync"
	"syscall/js"

	"github.com/0magnet/afero"
	"github.com/0magnet/seat"
	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
	"github.com/0magnet/websh/web"
)

func main() {
	container := js.Global().Get("document").Call("getElementById", "terminal")

	// Restore the filesystem before the shell opens on it.
	vfs := afero.NewMemMapFs()
	storage, serr := openStorage()
	persisted := false
	if serr == nil {
		if n, err := storage.loadAll(vfs); err == nil {
			persisted = true
			if n == 0 {
				if err := shell.Seed(vfs); err != nil {
					js.Global().Get("console").Call("error", "failed to seed filesystem: "+err.Error())
				}
			}
		}
	}
	if !persisted {
		if err := shell.Seed(vfs); err != nil {
			js.Global().Get("console").Call("error", "failed to seed filesystem: "+err.Error())
		}
	}

	registerBrowserApplets()

	var (
		syncMu sync.Mutex
		sigs   map[string]fileSig
	)
	opt := web.Options{
		FS:   vfs,
		Host: "user@websh",
	}
	if persisted {
		sigs = storage.syncFS(vfs, nil) // baseline snapshot, which also persists the seed
		// Every console flushes the one filesystem they share, one at a time.
		opt.AfterCommand = func() {
			syncMu.Lock()
			defer syncMu.Unlock()
			sigs = storage.syncFS(vfs, sigs)
		}

		shell.RegisterApplet("reset-fs", "wipe the persisted filesystem and reload",
			func(_ context.Context, _ *shell.Shell, hc *interp.HandlerContext, _ []string) int {
				if err := storage.clear(); err != nil {
					shell.Printf(hc.Stderr, "reset-fs: %v\n", err)
					return 1
				}
				shell.Println(hc.Stdout, "persisted filesystem cleared; reloading...")
				js.Global().Get("location").Call("reload")
				return 0
			})
	}

	// Consoles, as a machine has: Ctrl+Alt+1 to 4, each its own shell on the
	// one filesystem, started the first time it is switched to (0magnet/seat).
	s := seat.New(seat.Options{Root: container})
	for i := 1; i <= consoles; i++ {
		s.Add("tty"+strconv.Itoa(i), &console{opt: opt, link: i == 1})
	}
	if err := s.Show("tty1"); err != nil {
		js.Global().Get("console").Call("error", err.Error())
		return
	}
	select {}
}

// consoles is how many there are to switch between.
const consoles = 4

// console is one of the seat's screens: a shell, made when first shown.
type console struct {
	opt  web.Options
	link bool // opens a link a program made, which only the first does
	sess *web.Session
}

func (c *console) Mount(el js.Value) error {
	sess, err := web.NewSession(el, c.opt)
	if err != nil {
		return err
	}
	c.sess = sess
	// A link a program made opens with its command typed at the prompt.
	if c.link {
		sess.OpenLink()
	}
	return nil
}

func (c *console) Close() {
	if c.sess != nil {
		c.sess.Close()
	}
}
