//go:build js && wasm

// Package inside is for Go that runs as a shipped widget: a wasm module a
// program sent in its output (progressive.ShipWasm), which the host runs in
// a sandboxed document. It draws in that document as any Go program in a
// browser does (syscall/js), and talks to the program that placed it here.
package inside

import (
	"encoding/json"
	"syscall/js"
)

// Send gives v, as JSON, to the program, as a message event on its input
// (when it placed the widget with Events).
func Send(v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	js.Global().Get("websh").Call("send", js.Global().Get("JSON").Call("parse", string(b)))
	return nil
}

// OnMessage calls f with each message the program posts to this widget
// (progressive.Post), as JSON. f runs as a JS callback: it must not block.
func OnMessage(f func(data []byte)) {
	js.Global().Get("websh").Call("onmessage", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			f([]byte(js.Global().Get("JSON").Call("stringify", args[0]).String()))
		}
		return nil
	}))
}
