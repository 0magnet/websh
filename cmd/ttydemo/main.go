//go:build js && wasm

// ttydemo is a full-screen program for websh that is NOT compiled into the
// page: built on its own, fetched into the shell's filesystem and run from
// the PATH, as a separate wasm process with a terminal of its own.
//
//	curl -o /bin/ttydemo https://websh.magnetosphere.net/bin/ttydemo.wasm
//	ttydemo
//
// It draws in cells with tcell, follows the terminal's size, takes every key
// raw (Ctrl+C among them — q or Ctrl+C quits), and, where the terminal shows
// placements, lays html of its own over a box of its cells: a widget offered
// from this process, which the shell mounts and takes away again when it
// exits.
package main

import (
	"fmt"
	"io"
	"os"
	"syscall/js"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/0magnet/websh/childtty"
	"github.com/0magnet/websh/widget"
)

func main() {
	// Everything goes to the terminal: tcell's cells and this program's own
	// sequences alike, in the order written. (TinyGo holds os.Stdout back
	// until a newline, so a sequence printed there lands late, and torn.)
	var out io.Writer = os.Stdout
	var s tcell.Screen
	var err error
	if t, ok := childtty.Open(); ok {
		out = t
		s, err = tcell.NewTerminfoScreenFromTty(t)
	} else {
		s, err = tcell.NewScreen()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "ttydemo:", err)
		os.Exit(1)
	}
	if err := s.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "ttydemo:", err)
		os.Exit(1)
	}
	placed := widget.Shown()
	if placed {
		widget.Register("ttydemo", mount)
	}

	last := "none yet"
	keys := 0
	for {
		draw(s, out, placed, last, keys)
		switch ev := (<-s.EventQ()).(type) {
		case *tcell.EventResize:
			s.Sync()
		case *tcell.EventKey:
			keys++
			last = ev.Name()
			if ev.Key() == tcell.KeyCtrlC || ev.Str() == "q" {
				if placed {
					fmt.Fprint(out, "\x1b]7337;clear\x1b\\") //nolint:errcheck,gosec // the terminal; nowhere else to report to
				}
				s.Fini()
				fmt.Fprintf(out, "ttydemo: %d keys\n", keys) //nolint:errcheck,gosec // the terminal; nowhere else to report to
				// TinyGo keeps a js program alive after main returns, for its
				// callbacks; only an exit ends it.
				os.Exit(0)
			}
		}
	}
}

// draw lays out the screen: a frame, what the program knows, and the box the
// widget goes over.
func draw(s tcell.Screen, out io.Writer, placed bool, last string, keys int) {
	s.Clear()
	w, h := s.Size()
	frame := tcell.StyleDefault.Foreground(color.Teal)
	text := tcell.StyleDefault
	for x := 0; x < w; x++ {
		s.Put(x, 0, "─", frame)
		s.Put(x, h-1, "─", frame)
	}
	for y := 0; y < h; y++ {
		s.Put(0, y, "│", frame)
		s.Put(w-1, y, "│", frame)
	}
	s.Put(0, 0, "┌", frame)
	s.Put(w-1, 0, "┐", frame)
	s.Put(0, h-1, "└", frame)
	s.Put(w-1, h-1, "┘", frame)
	lines := []string{
		"ttydemo: a separate wasm process in websh",
		fmt.Sprintf("terminal %dx%d — resize the window", w, h),
		fmt.Sprintf("keys %d, last %s", keys, last),
		"q or Ctrl+C quits",
	}
	for i, l := range lines {
		s.PutStr(2, 1+i, l)
	}
	// The box: cells a terminal without placements shows, and the widget's
	// place where it has them.
	bx, by, bw, bh := 2, 6, min(36, w-4), min(8, h-8)
	if bw > 2 && bh > 2 {
		for y := by; y < by+bh; y++ {
			for x := bx; x < bx+bw; x++ {
				s.Put(x, y, "░", text.Foreground(color.Gray))
			}
		}
		s.PutStr(bx+1, by+1, "cells under the widget")
	}
	s.Show()
	if placed && bw > 2 && bh > 2 {
		d := fmt.Sprintf(`{"row":%d,"col":%d,"w":%d,"h":%d,"widget":"ttydemo"}`, by, bx, bw, bh)
		fmt.Fprint(out, "\x1b]7337;place;demo;"+b64(d)+"\x1b\\") //nolint:errcheck,gosec // the terminal; nowhere else to report to
	}
}

// mount fills a placement with html made in this process.
func mount(el js.Value) func() {
	doc := js.Global().Get("document")
	d := doc.Call("createElement", "div")
	d.Get("style").Set("cssText", "width:100%;height:100%;display:flex;align-items:center;justify-content:center;"+
		"font:14px sans-serif;color:#fff;background:linear-gradient(135deg,#0b7285,#5f3dc4)")
	d.Set("textContent", "html from ttydemo's own process")
	el.Call("append", d)
	return func() { d.Call("remove") }
}

func b64(s string) string {
	return js.Global().Call("btoa", s).String()
}
