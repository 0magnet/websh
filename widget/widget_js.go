//go:build js && wasm

// Package widget is how a program offers websh a widget to place over its
// cells (OSC 7337 place with "widget": name) when it is not compiled into the
// page: a program run from the filesystem, which is a wasm instance of its
// own. The page's own widgets go through web.RegisterWidget; this is the same
// offer from the other side of a process boundary.
//
// The registry is a plain object on the page, globalThis.webshWidgets, name ->
// {mount, owner}. Offering a widget is only a property written into it, never
// a call into websh, and websh calls mount (el) => unmount on a microtask of
// its own: two Go programs never run on one stack, which would corrupt both.
// What a program offered is withdrawn when it exits.
package widget

import (
	"syscall/js"

	"github.com/0magnet/bottle/proc"
)

// Global is the name of the registry on the page.
const Global = "webshWidgets"

func registry() js.Value {
	g := js.Global()
	r := g.Get(Global)
	if !r.Truthy() {
		r = g.Get("Object").New()
		g.Set(Global, r)
	}
	return r
}

// Register offers w as name. websh calls it with the element the placement
// fills, once that element is placed, and calls what it returns (if not nil)
// when the placement is taken away. w runs as a JS callback: it must not
// block.
func Register(name string, w func(el js.Value) (unmount func())) {
	mount := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		un := w(args[0])
		if un == nil {
			return nil
		}
		var f js.Func
		f = js.FuncOf(func(js.Value, []js.Value) any {
			un()
			f.Release()
			return nil
		})
		return f
	})
	e := js.Global().Get("Object").New()
	e.Set("mount", mount)
	e.Set("owner", owner())
	registry().Set(name, e)
}

// owner is this program's process id under bottle's proc, or "" for the page.
func owner() string {
	return proc.Getenv("BOTTLE_PID")
}

// Find returns the mount function offered as name.
func Find(name string) (js.Value, bool) {
	e := registry().Get(name)
	if !e.Truthy() || e.Get("mount").Type() != js.TypeFunction {
		return js.Value{}, false
	}
	return e.Get("mount"), true
}

// Drop withdraws every widget a process offered; websh calls it when the
// process exits.
func Drop(owner string) {
	if owner == "" {
		return
	}
	r := registry()
	keys := js.Global().Get("Object").Call("keys", r)
	for i := 0; i < keys.Length(); i++ {
		k := keys.Index(i).String()
		if e := r.Get(k); e.Truthy() && e.Get("owner").String() == owner {
			r.Delete(k)
		}
	}
}

// Mount calls a mount function found by Find with el, on a microtask, and
// hands done the unmount it returns: a function to call, through Unmount, or
// undefined.
func Mount(mount, el js.Value, done func(unmount js.Value)) {
	var cb js.Func
	cb = js.FuncOf(func(_ js.Value, args []js.Value) any {
		cb.Release()
		v := js.Undefined()
		if len(args) > 0 {
			v = args[0]
		}
		done(v)
		return nil
	})
	js.Global().Get("Promise").Call("resolve").
		Call("then", mount.Call("bind", js.Null(), el)).
		Call("then", cb, cb)
}

// Unmount calls an unmount function Mount handed over, on a microtask.
func Unmount(unmount js.Value) {
	if unmount.Type() == js.TypeFunction {
		js.Global().Get("Promise").Call("resolve").Call("then", unmount)
	}
}

// Shown reports whether this program's output goes to a websh terminal that
// shows placements: websh tells a program it runs so (WEBSH_PLACEMENTS=1).
// Elsewhere the sequences are ignored and the cells are the picture.
func Shown() bool {
	return proc.Getenv("WEBSH_PLACEMENTS") == "1"
}
