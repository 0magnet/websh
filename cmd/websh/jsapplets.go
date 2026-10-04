//go:build js && wasm

package main

// The browser applets — js, logs, download, upload, curl, nc, pbcopy,
// pbpaste — live in shell/browser so that any embedder gets them, not just
// this demo. See that package; this file only turns them on. mosh is its own
// package for the same reason.

import (
	"github.com/0magnet/websh/shell/browser"
	"github.com/0magnet/websh/shell/mosh"
)

func registerBrowserApplets() {
	browser.Register()
	mosh.Register()
}
