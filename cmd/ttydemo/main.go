//go:build js && wasm

// ttydemo is a progressive terminal program: a full-screen program for any
// terminal that does more where the host can. In websh it is NOT compiled
// into the page — it is built on its own, fetched into the shell's
// filesystem and run from the PATH, as a separate wasm process with a
// terminal of its own.
//
//	curl -o /bin/ttydemo https://websh.magnetosphere.net/bin/ttydemo.wasm
//	ttydemo
//
// It draws in cells with tcell, follows the terminal's size, and takes every
// key raw (q or Ctrl+C quits). It asks the host what it offers
// (progressive.Probe, through childtty) and shows the answer. Where the host
// shows placements, it lays html of its own over a box of its cells: a widget
// from this process with a button, and a line to the program both ways. A
// press reaches the program as an event on its input; the program counts it
// in its cells and posts the count back, which the widget shows. Clicks on
// the box are reported with the cell under them.
//
// Beside it is the same widget again, shipped: sent as a document in this
// program's output and run by the host in a sandboxed iframe. That is how a
// program on another machine, over ssh, puts its widgets in the tab; it
// talks over the same line.
package main

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"syscall/js"

	"github.com/gdamore/tcell/v3"
	"github.com/gdamore/tcell/v3/color"

	"github.com/0magnet/websh/childtty"
	"github.com/0magnet/websh/progressive"
	"github.com/0magnet/websh/widget"
)

// state is what the screen shows.
type state struct {
	placed  bool
	keys    int
	last    string
	presses int
	shipped int
	event   string
	did     string // what the last page key did
	mirror  string // the mirror last sent
}

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
	st := &state{placed: widget.Shown(), last: "none yet", event: "none yet"}
	say := func(seq string) { fmt.Fprint(out, seq) } //nolint:errcheck,gosec // the terminal; nowhere else to report to
	if st.placed {
		widget.RegisterConn("ttydemo", mount)
	}
	if progressive.Current().Has("ship") {
		for _, seq := range progressive.Ship("ttydemo-shipped", []byte(shippedWidget)) {
			say(seq)
		}
	}
	// The host's events join the keys in tcell's one queue.
	if evs := childtty.Events(); evs != nil {
		go func() {
			for e := range evs {
				s.EventQ() <- e
			}
		}()
	}

	for {
		draw(s, say, st)
		switch ev := (<-s.EventQ()).(type) {
		case *tcell.EventResize:
			s.Sync()
		case *progressive.Event:
			st.event = fmt.Sprintf("%s on %s", ev.Type, ev.ID)
			switch ev.Type {
			case "message":
				var m struct {
					Pressed bool `json:"pressed"`
				}
				if json.Unmarshal(ev.Data, &m) == nil && m.Pressed {
					if ev.ID == "shipped" {
						st.shipped++
						say(progressive.Post("shipped", map[string]int{"count": st.shipped}))
					} else {
						st.presses++
						say(progressive.Post("demo", map[string]int{"count": st.presses}))
					}
				}
				st.event += " " + string(ev.Data)
			default:
				st.event += fmt.Sprintf(" at cell %d,%d", ev.Col, ev.Row)
			}
		case *tcell.EventKey:
			st.keys++
			st.last = ev.Name()
			switch ev.Str() {
			case "t":
				say(progressive.Title(fmt.Sprintf("ttydemo: %d keys", st.keys)))
				st.did = "set the page's title"
			case "p":
				say(progressive.Page(fmt.Sprintf("/presses/%d", st.presses+st.shipped), "ttydemo"))
				st.did = "put /presses/N in the page's address: reload to open it again"
			case "d":
				say(progressive.Download("ttydemo.txt", []byte(fmt.Sprintf("ttydemo: %d keys, %d presses\n", st.keys, st.presses+st.shipped))))
				st.did = "offered ttydemo.txt"
			case "c":
				say(progressive.Copy(fmt.Sprintf("ttydemo counted %d presses", st.presses+st.shipped)))
				st.did = "copied a line to the clipboard"
			}
			if ev.Key() == tcell.KeyCtrlC || ev.Str() == "q" {
				if st.placed {
					say(progressive.Clear())
				}
				s.Fini()
				say(fmt.Sprintf("ttydemo: %d keys, %d presses, %d shipped presses\n", st.keys, st.presses, st.shipped))
				// TinyGo keeps a js program alive after main returns, for its
				// callbacks; only an exit ends it.
				os.Exit(0)
			}
		}
	}
}

// draw lays out the screen: a frame, what the program knows, and the box the
// widget goes over.
func draw(s tcell.Screen, say func(string), st *state) {
	s.Clear()
	w, h := s.Size()
	frame := tcell.StyleDefault.Foreground(color.Teal)
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
	host := "the host offers nothing beyond cells"
	if c := progressive.Current(); c != nil {
		host = fmt.Sprintf("host %s %s, trust %s, cell %.2fx%.2f px, offers %v", c.Host, c.Version, c.Trust, c.Cell.W, c.Cell.H, c.Features)
	}
	lines := []string{
		"ttydemo: a progressive terminal program, a separate wasm process in websh",
		fmt.Sprintf("terminal %dx%d — resize the window", w, h),
		fmt.Sprintf("keys %d, last %s", st.keys, st.last),
		host,
		fmt.Sprintf("presses counted here: offered widget %d, shipped widget %d; last event: %s", st.presses, st.shipped, st.event),
		"t title · p address (a link back here) · d download · c copy · q or Ctrl+C quits" + didNote(st.did),
		openedAt(),
	}
	for i, l := range lines {
		s.PutStr(2, 1+i, l)
	}
	// The box: cells a terminal without placements shows, and the widget's
	// place where it has them.
	bx, by, bw, bh := 2, 9, min(40, w-4), min(8, h-11)
	if bw > 2 && bh > 2 {
		for y := by; y < by+bh; y++ {
			for x := bx; x < bx+bw; x++ {
				s.Put(x, y, "░", tcell.StyleDefault.Foreground(color.Gray))
			}
		}
		s.PutStr(bx+1, by+1, "cells under the widget")
	}
	// The second box, for the shipped widget.
	sx, sw := bx+bw+2, min(40, w-(bx+bw+2)-2)
	if sw > 2 && bh > 2 {
		for y := by; y < by+bh; y++ {
			for x := sx; x < sx+sw; x++ {
				s.Put(x, y, "▒", tcell.StyleDefault.Foreground(color.Gray))
			}
		}
		s.PutStr(sx+1, by+1, "cells under the shipped one")
	}
	s.Show()
	if st.placed && bw > 2 && bh > 2 {
		say(progressive.Place("demo", progressive.Placement{Row: by, Col: bx, W: bw, H: bh, Widget: "ttydemo", Input: true, Events: true}))
	}
	if progressive.Current().Has("ship") && sw > 2 && bh > 2 {
		say(progressive.Place("shipped", progressive.Placement{Row: by, Col: sx, W: sw, H: bh, Widget: "ttydemo-shipped", Input: true, Events: true}))
	}
	// What the screen shows, as structure, for a screen reader.
	if progressive.Current().Has("mirror") {
		m := fmt.Sprintf("<h1>ttydemo</h1><p>A progressive terminal program in a %dx%d terminal.</p>"+
			"<ul><li>Keys pressed: %d, the last %s</li><li>Presses counted: offered widget %d, shipped widget %d</li>"+
			"<li>Last event: %s</li></ul><p>Keys: t title, p address, d download, c copy, q quit.</p>",
			w, h, st.keys, html.EscapeString(st.last), st.presses, st.shipped, html.EscapeString(st.event))
		if m != st.mirror {
			st.mirror = m
			for _, seq := range progressive.Mirror([]byte(m)) {
				say(seq)
			}
		}
	}
}

// shippedWidget is the widget sent as content: what a program on another
// machine would ship. It runs sandboxed, and has only websh.send and
// websh.onmessage.
const shippedWidget = `<!doctype html><meta charset="utf-8">
<style>html,body{margin:0;height:100%;font:14px sans-serif;color:#fff;
background:linear-gradient(135deg,#5f3dc4,#c2255c)}
body{display:flex;flex-direction:column;gap:8px;align-items:center;justify-content:center}
button{font:14px sans-serif;padding:6px 12px;cursor:pointer}</style>
<button>press: tell the program (shipped)</button>
<div id="said">shipped, sandboxed; the program has said nothing yet</div>
<script>
document.querySelector("button").onclick = () => websh.send({pressed: true});
websh.onmessage(m => { document.getElementById("said").textContent = "the program counted " + m.count; });
</script>`

// mount fills the placement with html made in this process: a button that
// tells the program, and a line showing what the program says back.
func mount(el js.Value, c *widget.Conn) func() {
	doc := js.Global().Get("document")
	d := doc.Call("createElement", "div")
	d.Get("style").Set("cssText", "width:100%;height:100%;display:flex;flex-direction:column;gap:8px;align-items:center;justify-content:center;"+
		"font:14px sans-serif;color:#fff;background:linear-gradient(135deg,#0b7285,#5f3dc4)")
	b := doc.Call("createElement", "button")
	b.Set("textContent", "press: tell the program")
	b.Get("style").Set("cssText", "font:14px sans-serif;padding:6px 12px;cursor:pointer")
	said := doc.Call("createElement", "div")
	said.Set("textContent", "the program has said nothing yet")
	d.Call("append", b, said)
	el.Call("append", d)

	press := js.FuncOf(func(js.Value, []js.Value) any {
		c.Send(map[string]bool{"pressed": true}) //nolint:errcheck,gosec // a host without a line drops it
		return nil
	})
	b.Call("addEventListener", "click", press)
	c.OnMessage(func(data []byte) {
		var m struct {
			Count int `json:"count"`
		}
		if json.Unmarshal(data, &m) == nil {
			said.Set("textContent", fmt.Sprintf("the program counted %d", m.Count))
		}
	})
	return func() {
		b.Call("removeEventListener", "click", press)
		press.Release()
		d.Call("remove")
	}
}

func didNote(did string) string {
	if did == "" {
		return ""
	}
	return "  — " + did
}

// openedAt says where a link that opened this program pointed in it.
func openedAt() string {
	if p := progressive.Current().LinkPath(); p != "" {
		return "opened by a link, at " + p
	}
	return ""
}
