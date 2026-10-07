//go:build js && wasm

package web

import (
	"strconv"
	"strings"
	"sync"
	"syscall/js"
)

// Placements: a program in the terminal lays an image, or a widget the page
// offers, over a rectangle of the terminal's cells, with no frame or
// chrome — html where it is needed, cells everywhere else. Like the viewer
// it is asked for with OSC 7337, so it is output like any other:
//
//	OSC 7337 ; place ; <id> ; <data> ST   lay <data> over its cells, or move it
//	OSC 7337 ; remove ; <id> ST           take it away
//	OSC 7337 ; clear ST                   take every placement away
//
// <data> is base64 of a JSON object: {"row": 2, "col": 40, "w": 30, "h": 12,
// and "url": "https://…" (an image) or "widget": "<name>"}, row and col from
// 0 at the top left of the screen. The program draws its own cells under it
// as ever, which a terminal without placements shows instead, and which show
// through where an image does not cover them. A placement takes no input: the
// mouse and the keys still go to the program. What a command placed is taken
// away when it ends.

// A Widget makes a widget's elements in el, the placement it fills, and
// returns what to do when the placement is taken away (nil for nothing).
type Widget func(el js.Value) (unmount func())

var (
	widgetsMu sync.Mutex
	widgets   = map[string]Widget{}
)

// RegisterWidget offers a widget by name to the programs in this page's
// terminals: OSC 7337 place with "widget": name lays it over their cells.
// Only what the page registers can be placed this way; a program can name a
// widget, never supply one.
func RegisterWidget(name string, w Widget) {
	widgetsMu.Lock()
	defer widgetsMu.Unlock()
	widgets[name] = w
}

func widget(name string) Widget {
	widgetsMu.Lock()
	defer widgetsMu.Unlock()
	return widgets[name]
}

// placeData is a placement's request.
type placeData struct {
	Row    int    `json:"row"`
	Col    int    `json:"col"`
	W      int    `json:"w"`
	H      int    `json:"h"`
	URL    string `json:"url"`
	Widget string `json:"widget"`
}

// A placement is one element over the cells.
type placement struct {
	d       placeData
	el      js.Value
	unmount func()
}

// placements is a session's layer of placements: one element over the
// terminal's screen, its children positioned in fractions of the screen,
// so they keep to their cells however the terminal is zoomed or its box
// resized.
type placements struct {
	s     *Session
	root  js.Value // the session's element, the terminal inside it
	layer js.Value
	by    map[string]*placement
	cols  int
	rows  int
}

func newPlacements(s *Session, root js.Value) *placements {
	return &placements{s: s, root: root, by: map[string]*placement{}}
}

// layerEl is the layer, made on first use inside the terminal's screen.
func (p *placements) layerEl() js.Value {
	if p.layer.Truthy() {
		return p.layer
	}
	screen := p.root.Call("querySelector", ".xterm-screen")
	if !screen.Truthy() {
		return js.Value{}
	}
	p.layer = js.Global().Get("document").Call("createElement", "div")
	p.layer.Get("style").Set("cssText", "position:absolute;inset:0;pointer-events:none;z-index:5;overflow:hidden")
	screen.Call("append", p.layer)
	return p.layer
}

// place lays d over its cells as id, replacing what id was.
func (p *placements) place(id string, d placeData) {
	layer := p.layerEl()
	if !layer.Truthy() || d.W <= 0 || d.H <= 0 {
		return
	}
	if old := p.by[id]; old != nil {
		if old.d.URL == d.URL && old.d.Widget == d.Widget {
			old.d = d // same content: only moved
			p.position(old)
			return
		}
		p.remove(id)
	}
	doc := js.Global().Get("document")
	pl := &placement{d: d}
	switch {
	case d.Widget != "":
		w := widget(d.Widget)
		if w == nil {
			return
		}
		pl.el = doc.Call("createElement", "div")
		pl.el.Get("style").Set("cssText", "position:absolute;overflow:hidden")
		layer.Call("append", pl.el)
		pl.unmount = w(pl.el)
	case strings.HasPrefix(d.URL, "https://") || strings.HasPrefix(d.URL, "http://"):
		pl.el = doc.Call("createElement", "img")
		pl.el.Get("style").Set("cssText", "position:absolute;object-fit:contain")
		pl.el.Set("src", d.URL)
		pl.el.Set("alt", "")
		layer.Call("append", pl.el)
	default:
		return
	}
	p.by[id] = pl
	p.position(pl)
}

// position sets pl's box from its cells and the screen's size now.
func (p *placements) position(pl *placement) {
	cols, rows := p.s.Term.Core.Cols(), p.s.Term.Core.Rows()
	if cols <= 0 || rows <= 0 {
		return
	}
	p.cols, p.rows = cols, rows
	pct := func(n, of int) string { return strconv.FormatFloat(100*float64(n)/float64(of), 'f', 4, 64) + "%" }
	st := pl.el.Get("style")
	st.Set("left", pct(pl.d.Col, cols))
	st.Set("top", pct(pl.d.Row, rows))
	st.Set("width", pct(pl.d.W, cols))
	st.Set("height", pct(pl.d.H, rows))
}

// reposition puts every placement back on its cells if the screen's
// columns or rows changed since they were placed.
func (p *placements) reposition() {
	if p.s.Term.Core.Cols() == p.cols && p.s.Term.Core.Rows() == p.rows {
		return
	}
	for _, pl := range p.by {
		p.position(pl)
	}
}

func (p *placements) remove(id string) {
	pl := p.by[id]
	if pl == nil {
		return
	}
	delete(p.by, id)
	if pl.unmount != nil {
		pl.unmount()
	}
	pl.el.Call("remove")
}

func (p *placements) clear() {
	for id := range p.by {
		p.remove(id)
	}
}
