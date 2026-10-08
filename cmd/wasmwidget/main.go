//go:build js && wasm

// wasmwidget is a widget shipped as wasm: Go, built for js/wasm, that a
// program sends in its output (progressive.ShipWasm) for the host to run in
// a sandbox. ttydemo ships it with its w key. It draws a turning pattern on
// a canvas — computed here, frame by frame — and has a button whose presses
// the program counts and answers.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"syscall/js"

	"github.com/0magnet/websh/widget/inside"
)

func main() {
	doc := js.Global().Get("document")
	body := doc.Get("body")
	body.Get("style").Set("cssText", "margin:0;height:100%;background:#111;color:#eee;font:13px sans-serif;display:flex;flex-direction:column")
	canvas := doc.Call("createElement", "canvas")
	canvas.Get("style").Set("cssText", "flex:1;width:100%;min-height:0")
	bar := doc.Call("createElement", "div")
	bar.Get("style").Set("cssText", "display:flex;gap:8px;align-items:center;padding:4px 8px")
	button := doc.Call("createElement", "button")
	button.Set("textContent", "press: tell the program (wasm)")
	said := doc.Call("createElement", "span")
	said.Set("textContent", "Go in a sandbox, shipped by the program")
	bar.Call("append", button, said)
	body.Call("append", canvas, bar)

	button.Call("addEventListener", "click", js.FuncOf(func(js.Value, []js.Value) any {
		inside.Send(map[string]bool{"pressed": true}) //nolint:errcheck,gosec // a map always marshals
		return nil
	}))
	inside.OnMessage(func(data []byte) {
		var m struct {
			Count int `json:"count"`
		}
		if json.Unmarshal(data, &m) == nil {
			said.Set("textContent", fmt.Sprintf("the program counted %d", m.Count))
		}
	})

	ctx := canvas.Call("getContext", "2d")
	var frame js.Func
	frame = js.FuncOf(func(_ js.Value, args []js.Value) any {
		t := args[0].Float() / 1000
		w, h := canvas.Get("clientWidth").Int(), canvas.Get("clientHeight").Int()
		if canvas.Get("width").Int() != w || canvas.Get("height").Int() != h {
			canvas.Set("width", w)
			canvas.Set("height", h)
		}
		ctx.Set("fillStyle", "#111")
		ctx.Call("fillRect", 0, 0, w, h)
		cx, cy, r := float64(w)/2, float64(h)/2, math.Min(float64(w), float64(h))*0.42
		for i := range 24 {
			a := t + float64(i)*math.Pi/12
			x, y := cx+r*math.Cos(a)*math.Cos(t*0.7), cy+r*math.Sin(a)
			ctx.Set("fillStyle", fmt.Sprintf("hsl(%d,80%%,60%%)", (i*15+int(t*40))%360))
			ctx.Call("beginPath")
			ctx.Call("arc", x, y, 4+3*math.Sin(t+float64(i)), 0, 2*math.Pi)
			ctx.Call("fill")
		}
		js.Global().Call("requestAnimationFrame", frame)
		return nil
	})
	js.Global().Call("requestAnimationFrame", frame)
	select {}
}
