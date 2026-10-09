// progdemo shows what a progressive terminal program can do, each thing
// beside what the same program does where the host cannot. It is one
// program, built for a desktop and for the browser alike:
//
//	go build ./cmd/progdemo                                    # a desktop terminal, or ssh into websh
//	tinygo build -target wasm -o progdemo.wasm ./cmd/progdemo  # run from websh's filesystem
//
// It asks its terminal what it offers (childtty, progressive.Probe) and then
// shows six pages: what the host answered; a picture in cells and as a real
// image (kitty's graphics, which websh and kitty both draw); a widget it
// ships, a slider that drives its cells while the arrow keys drive the slider;
// the page around the terminal (title, address, a link that opens the demo
// at a page); media (sound, notifications, the clipboard, downloads, files
// dropped in); and the accessible rendition it publishes for screen readers.
// Over ssh into websh the remote rules show: what a remote program may not
// do is refused, or asked of the person.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"strings"

	"github.com/gdamore/tcell/v3"
	tc "github.com/gdamore/tcell/v3/color"

	"github.com/0magnet/websh/childtty"
	"github.com/0magnet/websh/progressive"
)

// pages are the demo's pages: each has an address of its own in websh.
var pages = []struct{ name, title string }{
	{"host", "Host"},
	{"pictures", "Pictures"},
	{"widgets", "Widgets"},
	{"page", "The page"},
	{"media", "Media"},
	{"access", "Access"},
}

type demo struct {
	s    tcell.Screen
	out  io.Writer
	caps *progressive.Caps

	at       int    // the page shown
	hue      int    // the color the widgets page and the picture share
	note     string // what the last key or event did
	messages int    // messages from the shipped widget
	clicked  string // the last click on it, by cell
	dropped  string // the last file dropped in

	shipped   bool              // the widget has been sent
	placed    bool              // the widget is placed now
	picture   string            // the picture's box and hue as last sent, or ""
	pngs      map[string][]byte // pictures made, by box and hue
	mirrored  string            // the mirror last sent
	lastLines []string          // what the last page other than access published
}

func main() {
	var out io.Writer = os.Stdout
	var s tcell.Screen
	var err error
	if t, ok := childtty.Open(); ok {
		out = t // sequences of its own go where tcell's do, in order
		s, err = tcell.NewTerminfoScreenFromTty(t)
	} else {
		s, err = tcell.NewScreen()
	}
	if err == nil {
		err = s.Init()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "progdemo:", err)
		os.Exit(1)
	}
	d := &demo{s: s, out: out, caps: progressive.Current(), hue: 200, pngs: map[string][]byte{}}
	if p := strings.Trim(d.caps.LinkPath(), "/"); p != "" { // a link opened it at a page
		for i, pg := range pages {
			if pg.name == p {
				d.at = i
			}
		}
	}
	if d.caps.Has("drop") {
		d.say(progressive.ListenDrop())
	}
	if evs := childtty.Events(); evs != nil {
		go func() {
			for e := range evs {
				s.EventQ() <- e
			}
		}()
	}
	d.arrive()
	for {
		d.draw()
		switch ev := (<-s.EventQ()).(type) {
		case *tcell.EventResize:
			s.Sync()
			d.picture = "" // a cleared screen took the picture with it
		case *progressive.Event:
			d.event(ev)
		case *tcell.EventKey:
			if ev.Key() == tcell.KeyCtrlC || ev.Str() == "q" {
				d.quit()
			}
			d.key(ev)
		}
	}
}

func (d *demo) say(seq string) { fmt.Fprint(d.out, seq) } //nolint:errcheck,gosec // the terminal; nowhere else to report to

// has reports whether the host offers feature.
func (d *demo) has(feature string) bool { return d.caps.Has(feature) }

// remote is output the host knows came from another machine.
func (d *demo) remote() bool { return d.caps != nil && d.caps.Trust == "remote" }

// arrive is a page being opened: its title, its address, and the widget
// and picture of the page left taken away.
func (d *demo) arrive() {
	pg := pages[d.at]
	d.say(progressive.Title("progdemo: " + pg.title))
	if d.has("page") {
		d.say(progressive.Page("/"+pg.name, "progdemo: "+pg.title))
	}
	if d.placed && pg.name != "widgets" {
		d.say(progressive.Remove("slider"))
		d.placed = false
	}
	if d.picture != "" && pg.name != "pictures" {
		d.say(kittyDelete)
		d.picture = ""
	}
	d.note = ""
}

func (d *demo) key(ev *tcell.EventKey) {
	k, str := ev.Key(), ev.Str()
	switch {
	case k == tcell.KeyTab:
		d.at = (d.at + 1) % len(pages)
		d.arrive()
	case k == tcell.KeyBacktab:
		d.at = (d.at + len(pages) - 1) % len(pages)
		d.arrive()
	case len(str) == 1 && str[0] >= '1' && int(str[0]-'1') < len(pages):
		d.at = int(str[0] - '1')
		d.arrive()
	case k == tcell.KeyLeft || k == tcell.KeyRight:
		step := 15
		if k == tcell.KeyLeft {
			step = -15
		}
		d.hue = (d.hue + step + 360) % 360
		if d.placed {
			d.say(progressive.Post("slider", map[string]int{"hue": d.hue})) // the slider follows the keys
		}
		d.note = fmt.Sprintf("the keys set the hue to %d", d.hue)
	default:
		d.media(str)
	}
}

// media is the keys of the page and media pages.
func (d *demo) media(key string) {
	switch pages[d.at].name + ":" + key {
	case "page:i":
		d.say(progressive.Icon(icon))
		d.note = d.refusedIf(!d.has("page"), "set the page's icon", "a terminal with no page has no icon to set")
	case "media:s":
		if !d.has("sound") {
			d.note = "this terminal plays no sounds the program brings"
			return
		}
		for _, seq := range progressive.SoundData("beep", "audio/wav", beep(), 0.3, false) {
			d.say(seq)
		}
		d.note = "played a beep the program made itself"
	case "media:n":
		d.say(progressive.Notify("progdemo", "a notification from a terminal program"))
		d.note = "sent a notification (OSC 777, which many terminals show)"
	case "media:c":
		d.say(progressive.Copy("copied by progdemo"))
		d.note = "put a line on the clipboard (OSC 52)" + d.asked()
	case "media:d":
		d.say(progressive.Download("progdemo.txt", []byte(fmt.Sprintf("progdemo was here; the hue was %d\n", d.hue))))
		d.note = d.refusedIf(!d.has("download"), "offered progdemo.txt to save"+d.asked(), "this terminal saves no files a program offers")
	}
}

// asked is the note that the person was asked, as remote output is.
func (d *demo) asked() string {
	if d.remote() {
		return ": from another machine, so the person is asked first"
	}
	return ""
}

func (d *demo) refusedIf(refused bool, done, why string) string {
	if refused {
		return why
	}
	return done
}

func (d *demo) event(ev *progressive.Event) {
	switch ev.Type {
	case "message":
		var m struct {
			Hue *int `json:"hue"`
		}
		if json.Unmarshal(ev.Data, &m) == nil && m.Hue != nil {
			d.hue = (*m.Hue%360 + 360) % 360
			d.messages++
			d.say(progressive.Post("slider", map[string]int{"seen": d.messages, "hue": d.hue}))
			d.note = fmt.Sprintf("the slider set the hue to %d", d.hue)
		}
	case "drop":
		var f struct {
			Path string `json:"path"`
			Size int    `json:"size"`
		}
		if json.Unmarshal(ev.Data, &f) == nil {
			d.dropped = fmt.Sprintf("%s, %d bytes", f.Path, f.Size)
			d.note = "a file was dropped in: " + d.dropped
		}
	default:
		d.clicked = fmt.Sprintf("%s at cell %d,%d", ev.Type, ev.Col, ev.Row)
	}
}

func (d *demo) quit() {
	if d.placed {
		d.say(progressive.Clear())
	}
	if d.picture != "" {
		d.say(kittyDelete)
	}
	d.s.Fini()
	os.Exit(0) // TinyGo keeps a js program alive after main returns
}

// Styles.
var (
	stText   = tcell.StyleDefault
	stDim    = tcell.StyleDefault.Foreground(tc.Gray)
	stHead   = tcell.StyleDefault.Foreground(tc.Teal).Bold(true)
	stTab    = tcell.StyleDefault.Foreground(tc.Silver)
	stTabOn  = tcell.StyleDefault.Foreground(tc.White).Background(tc.Teal).Bold(true)
	stYes    = tcell.StyleDefault.Foreground(tc.Green)
	stNo     = tcell.StyleDefault.Foreground(tc.Olive)
	stNote   = tcell.StyleDefault.Foreground(tc.Yellow)
	stBorder = tcell.StyleDefault.Foreground(tc.DarkCyan)
)

// put writes s from x, y; it returns the column after it.
func (d *demo) put(x, y int, s string, st tcell.Style) int {
	w, _ := d.s.Size()
	for _, r := range s {
		if x >= w {
			break
		}
		d.s.Put(x, y, string(r), st)
		x++
	}
	return x
}

func (d *demo) box(x, y, w, h int) {
	for i := x; i < x+w; i++ {
		d.s.Put(i, y, "─", stBorder)
		d.s.Put(i, y+h-1, "─", stBorder)
	}
	for j := y; j < y+h; j++ {
		d.s.Put(x, j, "│", stBorder)
		d.s.Put(x+w-1, j, "│", stBorder)
	}
	d.s.Put(x, y, "┌", stBorder)
	d.s.Put(x+w-1, y, "┐", stBorder)
	d.s.Put(x, y+h-1, "└", stBorder)
	d.s.Put(x+w-1, y+h-1, "┘", stBorder)
}

func (d *demo) draw() {
	s := d.s
	s.Clear()
	w, h := s.Size()
	x := d.put(0, 0, " progdemo ", stHead)
	for i, pg := range pages {
		st := stTab
		if i == d.at {
			st = stTabOn
		}
		x = d.put(x+1, 0, fmt.Sprintf(" %d %s ", i+1, pg.title), st)
	}
	d.put(0, h-1, "Tab or 1–6 pages · ←/→ hue · q quit", stDim)
	if d.note != "" {
		d.put(0, h-2, d.note, stNote)
	}
	var lines []string
	switch pages[d.at].name {
	case "host":
		lines = d.drawHost()
	case "pictures":
		lines = d.drawPictures(w, h)
	case "widgets":
		lines = d.drawWidgets(w, h)
	case "page":
		lines = d.drawPage()
	case "media":
		lines = d.drawMedia()
	case "access":
		lines = d.drawAccess()
	}
	s.Show()
	d.after(w, h)
	d.mirror(lines)
}

// row writes a capability: what it is, and what it does in this terminal.
func (d *demo) row(y int, what string, ok bool, here string) {
	d.put(2, y, what, stText)
	st, mark := stNo, "· "
	if ok {
		st, mark = stYes, "✓ "
	}
	d.put(26, y, mark+here, st)
}

func (d *demo) drawHost() []string {
	y := 2
	answer := "nothing beyond cells: every page falls back to them"
	switch c := d.caps; {
	case c != nil && c.V > 0:
		answer = fmt.Sprintf("%s %s, a progressive host; output trusted as %q, cells %.1f×%.1f px", c.Host, c.Version, c.Trust, c.Cell.W, c.Cell.H)
	case c != nil:
		name := c.Host
		if name == "" {
			name = "a terminal"
		}
		answer = fmt.Sprintf("%s, not a progressive host; it has %s of its own", name, strings.ReplaceAll(strings.Join(c.Features, " and "), "kitty-graphics", "kitty's graphics"))
	}
	d.put(2, y, "This terminal answered: "+answer, stText)
	if d.has("place") {
		d.put(2, y+1, "It offers: "+strings.Join(d.caps.Features, " "), stDim)
	}
	y += 3
	d.put(2, y, "So here:", stHead)
	y++
	pic := "the picture is drawn in cells"
	if d.has("kitty-graphics") {
		pic = "a real picture, laid over its cells"
	}
	d.row(y, "2 Pictures", d.has("kitty-graphics"), pic)
	wid := "the arrow keys are the control"
	if d.has("ship") {
		wid = "a slider the program sends, talking both ways"
	}
	d.row(y+1, "3 Widgets", d.has("ship"), wid)
	pg := "the window's title only"
	if d.has("page") {
		pg = "title, address and links into the program"
	} else if d.remote() {
		pg = "refused: a remote program may not change the page"
	}
	d.row(y+2, "4 The page", d.has("page"), pg)
	md := "notifications and the clipboard, if the terminal allows"
	if d.has("sound") {
		md = "sound, notifications, clipboard, downloads, file drops"
	}
	d.row(y+3, "5 Media", d.has("sound"), md)
	ac := "nothing: a screen reader reads a grid of characters"
	if d.has("mirror") {
		ac = "a structured rendition, read by screen readers"
	}
	d.row(y+4, "6 Access", d.has("mirror"), ac)
	return []string{"progdemo, a progressive terminal program.", "This terminal answered: " + answer + ".", "Pictures: " + pic + ".", "Widgets: " + wid + ".", "The page: " + pg + ".", "Media: " + md + ".", "Access: " + ac + "."}
}

// picBox is where the pictures page draws: the cells version left, the
// picture right.
func picBox(w, h int) (bw, bh int) {
	bw = min((w-6)/2, 60)
	bh = min(h-10, bw/2) // the hue line under it stays above the note
	return bw, bh
}

func (d *demo) drawPictures(w, h int) []string {
	bw, bh := picBox(w, h)
	d.put(2, 2, "The same picture, drawn by the program: in cells (half blocks), and as an image.", stText)
	if bw < 8 || bh < 4 {
		d.put(2, 4, "make the terminal bigger to see them", stNote)
		return []string{"The pictures page: the terminal is too small to show them."}
	}
	d.box(2, 4, bw+2, bh+2)
	d.halfBlocks(3, 5, bw, bh)
	d.put(3, 4, " cells ", stDim)
	rx := 2 + bw + 4
	d.box(rx, 4, bw+2, bh+2)
	if d.has("kitty-graphics") {
		d.put(rx+1, 4, " image ", stDim)
	} else {
		d.put(rx+2, 6, "This terminal shows no pictures:", stNo)
		d.put(rx+2, 7, "the cells at left are the picture.", stNo)
	}
	d.put(2, bh+7, fmt.Sprintf("hue %d — ←/→ turn it, and the picture is made again", d.hue), stDim)
	return []string{"The pictures page: a Mandelbrot set drawn by the program, in character cells and, where the terminal can, as an image.", fmt.Sprintf("The hue is %d.", d.hue)}
}

func (d *demo) drawWidgets(w, h int) []string {
	d.put(2, 2, "A widget the program sends and the host runs, sandboxed. It and the program talk both ways.", stText)
	bar := min(w-6, 48)
	d.put(2, 4, "The program's cells, in the slider's color:", stDim)
	for i := 0; i < bar; i++ {
		c := hsv(float64(d.hue), 0.2+0.8*float64(i)/float64(bar), 0.95)
		d.s.Put(2+i, 5, " ", tcell.StyleDefault.Background(tc.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))))
	}
	d.put(2, 6, fmt.Sprintf("hue %d · messages from the widget %d · ←/→ move the slider from here", d.hue, d.messages), stText)
	if d.clicked != "" {
		d.put(2, 7, "last click on it: "+d.clicked, stDim)
	}
	if !d.has("ship") {
		d.put(2, 9, "This terminal takes no widgets: the arrow keys are the control, and the cells show it.", stNo)
	} else if h > 16 {
		d.box(2, 9, min(w-4, 52), 6)
	}
	return []string{"The widgets page: a slider the program sent, which sets the color of the program's cells; the arrow keys set it too.", fmt.Sprintf("The hue is %d; the widget has sent %d messages.", d.hue, d.messages)}
}

func (d *demo) drawPage() []string {
	d.put(2, 2, "A program can name the page it is on, and be linked to there.", stText)
	y := 4
	switch {
	case d.has("page"):
		d.put(2, y, "Each page of this demo has an address of its own: look at the address bar.", stYes)
		d.put(2, y+1, "Reload, or send the link: progdemo opens again on the same page.", stYes)
		d.put(2, y+2, "i sets the page's icon.", stText)
	case d.remote():
		d.put(2, y, "From another machine, the page is not the program's to change: the host refused the address.", stNo)
		d.put(2, y+1, "The window's title (OSC 2) is the terminal's own, so it is refused too.", stNo)
	default:
		d.put(2, y, "This terminal has no page: the window's title (OSC 2) is set, if it shows one.", stNo)
	}
	if p := d.caps.LinkPath(); p != "" {
		d.put(2, y+4, "This run was opened by a link, at "+p, stNote)
	}
	return []string{"The page: each demo page has an address and a title of its own, and a link opens the demo there."}
}

func (d *demo) drawMedia() []string {
	d.put(2, 2, "Things beside the cells, each over a standard sequence other terminals know too.", stText)
	keys := []struct{ k, what, feature, without string }{
		{"s", "play a sound the program made", "sound", "no: a terminal plays no sound a program brings"},
		{"n", "send a notification", "notify", "maybe: many terminals show OSC 777"},
		{"c", "copy a line to the clipboard", "clipboard", "maybe: OSC 52, if the terminal allows it"},
		{"d", "offer a file to save", "download", "maybe: iTerm2 saves OSC 1337 files; most do not"},
	}
	for i, k := range keys {
		d.put(2, 4+i, k.k+"  "+k.what, stText)
		if d.has(k.feature) {
			d.put(36, 4+i, "✓ yes"+d.asked(), stYes)
		} else {
			d.put(36, 4+i, "· "+k.without, stNo)
		}
	}
	drop := "this terminal sends no dropped files"
	if d.has("drop") {
		drop = "drop a file on the terminal: it comes to the program"
	}
	d.put(2, 9, drop, stDim)
	if d.dropped != "" {
		d.put(2, 10, "dropped: "+d.dropped, stYes)
	}
	return []string{"Media: keys s, n, c and d play a sound, notify, copy and offer a file; files dropped on the terminal reach the program."}
}

func (d *demo) drawAccess() []string {
	d.put(2, 2, "Cells are not structure. The program also publishes what it shows, as HTML, for screen readers.", stText)
	if !d.has("mirror") {
		d.put(2, 4, "This terminal takes none: a screen reader reads its grid of characters.", stNo)
		return nil
	}
	d.put(2, 4, "Here is what a screen reader is given for the page you came from:", stDim)
	y := 6
	for _, l := range d.lastLines {
		if y > 20 {
			break
		}
		d.put(4, y, "• "+l, stText)
		y++
	}
	d.put(2, y+1, "In websh, a11y on turns on the screen reader mode that reads it.", stDim)
	return []string{"Access: this page shows what a screen reader is given for the others."}
}

// after lays what goes over the cells, once they are drawn.
func (d *demo) after(w, h int) {
	switch pages[d.at].name {
	case "pictures":
		if !d.has("kitty-graphics") {
			return
		}
		bw, bh := picBox(w, h)
		if bw < 8 || bh < 4 {
			return
		}
		key := fmt.Sprintf("%dx%d/%d", bw, bh, d.hue)
		if key == d.picture {
			return
		}
		d.picture = key
		png, ok := d.pngs[key]
		if !ok {
			cw, ch := 9.0, 18.0
			if d.caps != nil && d.caps.Cell.W > 0 {
				cw, ch = d.caps.Cell.W, d.caps.Cell.H
			}
			png = mandelbrotPNG(int(float64(bw)*cw), int(float64(bh)*ch), d.hue)
			d.pngs[key] = png
		}
		d.say(kittyPicture(5, 2+bw+5, bw, bh, png))
	case "widgets":
		if !d.has("ship") || h <= 16 {
			return
		}
		if !d.shipped {
			for _, seq := range progressive.Ship("progdemo-slider", []byte(sliderWidget)) {
				d.say(seq)
			}
			d.shipped = true
		}
		if !d.placed {
			d.say(progressive.Place("slider", progressive.Placement{Row: 10, Col: 3, W: min(w-4, 52) - 2, H: 4, Widget: "progdemo-slider", Input: true, Events: true}))
			d.say(progressive.Post("slider", map[string]int{"hue": d.hue}))
			d.placed = true
		}
	}
}

// mirror publishes the page's lines, and keeps them for the access page.
func (d *demo) mirror(lines []string) {
	if pages[d.at].name != "access" {
		d.lastLines = lines
	}
	if !d.has("mirror") {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<h1>progdemo: %s</h1>", html.EscapeString(pages[d.at].title))
	for _, l := range lines {
		fmt.Fprintf(&b, "<p>%s</p>", html.EscapeString(l))
	}
	if b.String() == d.mirrored {
		return
	}
	d.mirrored = b.String()
	for _, seq := range progressive.Mirror([]byte(d.mirrored)) {
		d.say(seq)
	}
}

// halfBlocks draws the picture in w×h cells, two pixels a cell.
func (d *demo) halfBlocks(x, y, w, h int) {
	for j := 0; j < h; j++ {
		for i := 0; i < w; i++ {
			top := mandel(float64(i)/float64(w), float64(2*j)/float64(2*h), d.hue)
			bot := mandel(float64(i)/float64(w), float64(2*j+1)/float64(2*h), d.hue)
			d.s.Put(x+i, y+j, "▀", tcell.StyleDefault.
				Foreground(tc.NewRGBColor(int32(top.R), int32(top.G), int32(top.B))).
				Background(tc.NewRGBColor(int32(bot.R), int32(bot.G), int32(bot.B))))
		}
	}
}

// mandel is the color of the Mandelbrot set at u, v in the unit square.
func mandel(u, v float64, hue int) color.RGBA {
	cr, ci := -2.2+u*3.0, -1.2+v*2.4
	zr, zi := 0.0, 0.0
	const most = 60
	n := 0
	for ; n < most && zr*zr+zi*zi < 4; n++ {
		zr, zi = zr*zr-zi*zi+cr, 2*zr*zi+ci
	}
	if n == most {
		return color.RGBA{10, 10, 20, 255}
	}
	t := float64(n) / most
	return hsv(float64(hue)+t*120, 0.85, 0.25+0.75*math.Sqrt(t))
}

func mandelbrotPNG(w, h, hue int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, mandel(float64(x)/float64(w), float64(y)/float64(h), hue))
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img) //nolint:errcheck,gosec // into memory
	return b.Bytes()
}

func hsv(h, s, v float64) color.RGBA {
	h = math.Mod(h, 360) / 60
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h, 2)-1))
	var r, g, b float64
	switch int(h) {
	case 0:
		r, g = c, x
	case 1:
		r, g = x, c
	case 2:
		g, b = c, x
	case 3:
		g, b = x, c
	case 4:
		r, b = x, c
	default:
		r, b = c, x
	}
	m := v - c
	return color.RGBA{uint8((r + m) * 255), uint8((g + m) * 255), uint8((b + m) * 255), 255}
}

// pictureID is the kitty image the pictures page shows, under one id so
// each sends again over the last.
const pictureID = 7337

var kittyDelete = fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", pictureID)

// kittyPicture lays a PNG over w×h cells at row, col (zero-based) with kitty's
// graphics protocol, the cursor kept where it was.
func kittyPicture(row, col, w, h int, data []byte) string {
	enc := base64.StdEncoding.EncodeToString(data)
	var b strings.Builder
	fmt.Fprintf(&b, "\x1b7\x1b[%d;%dH", row+1, col+1)
	first := true
	for len(enc) > 0 || first {
		n := min(len(enc), 4096)
		more := 0
		if n < len(enc) {
			more = 1
		}
		if first {
			fmt.Fprintf(&b, "\x1b_Ga=T,f=100,i=%d,c=%d,r=%d,C=1,q=2,m=%d;%s\x1b\\", pictureID, w, h, more, enc[:n])
			first = false
		} else {
			fmt.Fprintf(&b, "\x1b_Gm=%d;%s\x1b\\", more, enc[:n])
		}
		enc = enc[n:]
	}
	b.WriteString("\x1b8")
	return b.String()
}

// sliderWidget is the widget the program ships: a document the host runs in
// a sandbox, with only websh.send and websh.onmessage to the program.
const sliderWidget = `<!doctype html><meta charset="utf-8">
<style>html,body{margin:0;height:100%;font:14px sans-serif;color:#fff;background:#1b2330}
body{display:flex;gap:12px;align-items:center;padding:0 12px;box-sizing:border-box}
input{flex:1}#said{min-width:16em;color:#9fd}</style>
<label for=h>hue</label><input id=h type=range min=0 max=359 value=200>
<span id=said>sent by the program; it has said nothing yet</span>
<script>
const h = document.getElementById("h"), said = document.getElementById("said");
h.oninput = () => websh.send({hue: +h.value});
websh.onmessage(m => {
  if (m.hue !== undefined && document.activeElement !== h) h.value = m.hue;
  said.textContent = m.seen ? "the program drew hue " + m.hue + " (message " + m.seen + ")" : "the program set hue " + m.hue;
});
</script>`

// icon is the favicon progdemo sets: a teal square with a P.
const icon = "data:image/svg+xml," +
	"%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 16 16'%3E%3Crect width='16' height='16' rx='3' fill='%230b7285'/%3E" +
	"%3Ctext x='8' y='12.5' font-size='12' text-anchor='middle' fill='white' font-family='sans-serif'%3EP%3C/text%3E%3C/svg%3E"

// beep is a fifth of a second of 660 Hz as a WAV file made here: the
// program brings its own sounds.
func beep() []byte {
	const rate, n = 8000, 1600
	b := make([]byte, 44+n)
	le := binary.LittleEndian
	copy(b, "RIFF")
	le.PutUint32(b[4:], 36+n)
	copy(b[8:], "WAVEfmt ")
	le.PutUint32(b[16:], 16)
	le.PutUint16(b[20:], 1) // PCM
	le.PutUint16(b[22:], 1) // mono
	le.PutUint32(b[24:], rate)
	le.PutUint32(b[28:], rate)
	le.PutUint16(b[32:], 1)
	le.PutUint16(b[34:], 8)
	copy(b[36:], "data")
	le.PutUint32(b[40:], n)
	for i := range n {
		v := 128 + 40
		if (i*660*2/rate)%2 == 1 { // a square wave
			v = 128 - 40
		}
		b[44+i] = byte(v)
	}
	return b
}
