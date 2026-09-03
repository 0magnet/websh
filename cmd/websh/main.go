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
	"syscall/js"

	"github.com/0magnet/afero"
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

	var sigs map[string]fileSig
	opt := web.Options{
		FS:       vfs,
		Host:     "user@websh",
		Greeting: greeting(persisted),
	}
	if persisted {
		sigs = storage.syncFS(vfs, nil) // baseline snapshot, which also persists the seed
		opt.AfterCommand = func() { sigs = storage.syncFS(vfs, sigs) }

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

	if _, err := web.NewSession(container, opt); err != nil {
		js.Global().Get("console").Call("error", err.Error())
		return
	}
	select {}
}

func greeting(persisted bool) string {
	fsNote := "an in-memory filesystem (IndexedDB unavailable — changes are not persisted)"
	if persisted {
		fsNote = "a filesystem persisted in IndexedDB — your files survive reloads (\x1b[1mreset-fs\x1b[0m to wipe)"
	}
	return "\x1b[1;36mwebsh\x1b[0m — bash in your browser · \x1b[2mgithub.com/0magnet/websh\x1b[0m\r\n" +
		"the shell is \x1b[1m0magnet/sh\x1b[0m (mvdan/sh fork) on " + fsNote + "\r\n" +
		"try: \x1b[1mhelp\x1b[0m · \x1b[1mcat readme.md\x1b[0m · \x1b[1msource demo.sh\x1b[0m · \x1b[1mtree /\x1b[0m\r\n\r\n"
}
