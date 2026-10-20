//go:build js && wasm

// Command websh-term is the terminal package term serves: websh with the pty
// applet, opened on a page that names the command to start with. Its
// filesystem is in memory and goes with the page.
//
// The start command comes from the page's own markup, which its server
// wrote, never from the address: a link that ran commands would let anyone's
// page reach this shell (see web.OpenLink, which only types one).
package main

import (
	"syscall/js"

	"github.com/0magnet/websh/shell/pty"
	"github.com/0magnet/websh/web"
)

func main() {
	doc := js.Global().Get("document")
	console := js.Global().Get("console")
	pty.Register()

	opt := web.Options{Host: meta(doc, "websh-host")}
	if opt.Host == "" {
		opt.Host = "websh"
	}
	sess, err := web.NewSession(doc.Call("getElementById", "terminal"), opt)
	if err != nil {
		console.Call("error", "websh-term: "+err.Error())
		return
	}
	if run := meta(doc, "websh-run"); run != "" {
		sess.Submit(run)
	}
	select {}
}

// meta is the content of the page's <meta name=name>, or "".
func meta(doc js.Value, name string) string {
	el := doc.Call("querySelector", `meta[name="`+name+`"]`)
	if el.IsNull() {
		return ""
	}
	return el.Call("getAttribute", "content").String()
}
