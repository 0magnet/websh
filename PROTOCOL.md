# The progressive terminal

A **progressive terminal** program is a terminal program first: it draws in
cells and reads keys, and it runs in any terminal. Where its host can do
more, it asks for more over its own output, and becomes more: pictures and
working widgets over its cells, its own font, the page's title and address.
The host realizes as much of the one program as it can; the program never
has two interfaces. websh is such a host — a **capable host** — and this
document is the protocol between the two.

A program in a terminal writes text and reads keys. In websh the terminal is
a web page, so the page can do more for the program than draw cells: lay a
picture or a working widget over them, use a font the program brings, give
the page a title and an address, take a file, play a sound. This document is
how a program asks for those things, and how it learns what it may ask for.

It is written so that one program serves both worlds. The same binary is an
ordinary TUI in any terminal and a richer one where the host can do more; the
richer parts are only ever additions, and the cells beneath them are always
drawn. Nothing here requires a second, "web" version of a program.

Status markers: **built** (in websh now), **next** (being built), **planned**
(designed here, not yet built).

## Principles

1. **Output is the only channel out, input the only channel back.** A program
   asks by writing escape sequences to its terminal and hears back as input,
   exactly as it does for a cursor position or a terminal name today. So the
   protocol works wherever the program's output goes: compiled into the page,
   run as a process in the tab, or on another machine over ssh, mosh or
   skywire.
2. **Cells first.** Every request has a cell fallback the program draws anyway.
   A terminal that does not know a sequence ignores it (unknown OSCs are
   discarded by every mainstream terminal), and the program is unchanged.
3. **Ask, don't assume.** A program learns what the host offers by querying it
   (Discovery), never from environment variables or from the name of the
   terminal; those do not cross ssh, and replies do.
4. **Trust follows the source.** What a request may do depends on where the
   bytes came from: the page's own program, a program the page launched, or
   a remote machine. A remote program may decorate its own cells; it may not
   change the person's terminal or the page around it (Trust).
5. **A program's changes end with it.** Placements, a font, widgets: all are
   undone when the command that asked for them exits, as the alternate screen
   and the cursor are.
6. **Use the standard where one exists.** Hyperlinks, clipboard,
   notifications, images, keyboard and file transfer already have protocols
   (OSC 8, OSC 52, OSC 9/99/777, iTerm2 1337, kitty's). websh implements those
   rather than inventing its own, so existing programs work as they are. OSC
   7337 covers only what has no precedent.

## Sequences

    OSC 7337 ; <verb> [ ; <id> ] [ ; <data> ] ST

OSC is `ESC ]`, ST is `ESC \` (BEL is accepted too). `<data>` is standard
base64 of a JSON object. `<id>` names something the program made, so it can
change or remove it; ids are the program's own and live only as long as it.

Large payloads (a font, a shipped widget) are sent in chunks of at most 4096
base64 bytes: every chunk but the last carries `"more": true`, and the host
assembles them in order, as kitty's graphics protocol does.

Replies come back as input:

    OSC 7337 ; <verb> ; <data> ST

## Trust

The host gives every byte of output one of four sources:

| Source | What it is |
|---|---|
| **page** | the program the page itself is (compiled into the page's wasm) |
| **local** | a program the shell launched from its filesystem (a process in the tab) |
| **remote** | output arriving over ssh, mosh, or any network session |
| **pipe** | output that is not going to the terminal at all (`prog \| less`): no requests apply |

| Request | page | local | remote |
|---|---|---|---|
| Discovery, cell metrics | yes | yes | yes |
| Placements: images (http/https) | yes | yes | yes |
| Placements: widgets the program offers in the tab | yes | yes | no (it is not in the tab) |
| Shipped widgets (sandboxed) | yes | yes | yes |
| Events back from placements | yes | yes | yes |
| Font | yes | yes | no |
| Page: title, address, favicon | yes | yes | no |
| Downloads offered, files dropped | yes | yes | ask the person |
| Notifications | yes | yes | yes, rate-limited |
| Clipboard write (OSC 52) | yes | yes | ask the person |
| Clipboard read | no | no | no |

The reply to Discovery says which source the host saw (`"trust"`), so a
program does not ask for what it will not get.

**Built:** the shell marks the output source as a command runs (Go:
`Shell.WithSource`): a program from the filesystem is local, ssh and mosh are
remote, everything else is page. Remote output may not mount a widget a
program in the tab offered. The other rows apply as their features are built.

## Discovery

**Built.**

    query:  OSC 7337 ; caps ? ST
    reply:  OSC 7337 ; caps ; <data> ST

```json
{
  "v": 1,
  "host": "websh",
  "version": "<module version>",
  "trust": "local",
  "cell": { "w": 8.99, "h": 19.7 },
  "dpr": 1.0156,
  "cols": 208,
  "rows": 47,
  "features": ["place", "place.input", "event", "post", "widget.offer"]
}
```

`cell` is the cell size in CSS pixels, `dpr` the device pixel ratio: what a
program needs to draw a picture that fits its cells exactly (a half-block
image, say, whose pixels are half a cell tall and not square).

A program that does not know whether it is in websh at all sends the query
and then DA1 (`CSI c`), which every terminal answers, and reads its input
until the DA1 reply arrives. If the caps reply came first, the host speaks
this protocol; if only DA1 came, it does not. No timeout is needed, and
nothing is guessed. The Go package `progressive` does exactly this (`progressive.Probe`),
and hands back the terminal's input with nothing lost: keys typed during the
probe, and the read it had waiting. `childtty.Open` runs it before tcell takes
the terminal; `progressive.Current` has the answer.

`features` lists only what the host does now, for this program's trust. So
far: `place` (images), `place.input`, `event`, `post`, `ship`, `download`,
`clipboard`, and `widget.offer`, `font`, `title`, `page` (not for remote
output).

Standard queries a program may also use, answered by websh's terminal
(xterm-go):

| Query | Reply | Status |
|---|---|---|
| DA1 `CSI c` | `CSI ? 1 ; 2 c` | built |
| DA2 `CSI > c` | `CSI > 0 ; 276 ; 0 c` | built |
| XTVERSION `CSI > q` | `DCS > \| websh ST` | built |
| Cell size `CSI 16 t` | `CSI 6 ; <h> ; <w> t` (device pixels) | built |
| Text area `CSI 14 t` | `CSI 4 ; <h> ; <w> t` (device pixels) | built |
| Size in cells `CSI 18 t` | `CSI 8 ; <rows> ; <cols> t` | built |
| Colors OSC 4/10/11/12 `?` | `OSC 10 ; rgb:rrrr/gggg/bbbb ST` | built |
| DSR, DECRQM, DECRQSS | standard | built |

## Placements

**Built.** A placement lays an image or a widget over a rectangle of the
program's cells, borderless, positioned in cells, so it keeps to them through
zoom and resize.

    OSC 7337 ; place ; <id> ; <data> ST
    OSC 7337 ; remove ; <id> ST
    OSC 7337 ; clear ST

| Field | Meaning |
|---|---|
| `row`, `col`, `w`, `h` | the cells covered, from 0 at the top left |
| `url` | an http(s) image, or |
| `widget` | a widget by name: one the page registered, or one the program offered (below) |
| `fit` | for an image: `contain` (default), `cover`, `fill` |
| `input` | `true`: the mouse over it is the placement's, not the program's |
| `events` | `true`: what happens to it is reported to the program (Events) |

The program keeps drawing its cells under every placement; that is what a
terminal without placements shows. Placements go when the command ends.

### Widgets a program offers

**Built** for page and local programs. A program running in the tab offers a
widget by writing it into a plain registry on the page,
`globalThis.webshWidgets[name] = {mount, owner}` (Go: `widget.Register`), and
places it by name. websh calls `mount(el, port)` on a microtask of its own,
never from its own stack: the program is a separate Go runtime, and two on
one stack corrupt each other. `port` is the widget's end of its line to the
program (Events); a widget that does not talk ignores it (Go:
`widget.RegisterConn` gives it as a `*widget.Conn`). What a program offered
is withdrawn when it exits. `widget.Shown()` tells a program its terminal
shows placements, from Discovery.

### Shipped widgets

**Built.** A program on another machine cannot offer a function, but it can
send a widget as content:

    OSC 7337 ; ship ; <name> ; <data> ; <chunk> ST

`<data>` is `{"kind": "html", "more": bool}`, and the chunks (at most 4096
base64 bytes each, every one but the last with `"more": true`) are the
widget: a whole HTML document. Its scripts may fetch and run anything a
page may, wasm included, from servers that allow it (CORS). The host runs
it in an `<iframe sandbox="allow-scripts">`: an opaque origin, so it can
draw and compute but cannot reach the page, its storage or its cookies, and
the page cannot reach into it. It talks only to the program, over the same
line as any widget, which the host transfers in; a script the host puts
first gives the document

    websh.send(value)      // to the program, as a message event
    websh.onmessage(f)     // f(value) for each post from the program

Placing it by name works as for any widget, with the page's own widgets
first, then shipped ones, then offered ones. Clicks inside it stay inside
it, so a shipped widget reports what was done to it by sending. A command
may ship 8 MB in all; what it shipped goes when it ends. Go:
`progressive.Ship(name, html)` gives the sequences.

This is what makes a progressive terminal program work over ssh: its
widgets drawn on its behalf in the person's tab, by whichever machine it
runs on. `cmd/ttydemo` ships one beside the one it offers. A wasm module
shipped as its own kind (rather than loaded by a shipped document) is
planned.

## Events

**Built.** A placement made with `"events": true` reports what happens to it on
the program's input, as a terminal reports a mouse click:

    OSC 7337 ; event ; <id> ; <data> ST

| `type` | From | Fields |
|---|---|---|
| `click`, `dblclick`, `contextmenu` | the host, for a placement with `input` | `x`, `y` (fractions of the placement), `col`, `row` (the cell there, on the screen), `button` |
| `message` | the widget in it | `data`: what the widget sent, as JSON |

The program sends its widget a message:

    OSC 7337 ; post ; <id> ; <data> ST

`<data>` is JSON, given to the widget as it is.

**The line.** Every widget a program offers gets a `MessagePort` (the host's
`MessageChannel`, one per placement): what the widget posts there is the
`data` of a message event, and a program's `post` arrives there. Messages are
JSON text, both ways, so any language can be on either end. A port delivers
later by itself, so the widget, the host and the program never run inside
one another's call, and it is what a sandboxed iframe uses too: a shipped
widget will speak the same line.

Events are for the command that asked: one that arrives when nothing is
running is dropped, and one the command never reads goes when it ends, as
replies do. Programs that never asked for events never see one, and a host
that offers none does not list `event` in Discovery.

**Reading them.** In Go, `progressive.Filter(r)` takes the events out of a
terminal's input — what was typed passes through untouched, an event split
across reads is put back together, and a lone Escape is never held back —
and delivers them on a channel. `childtty` applies it (`childtty.Events()`).
An event is a `tcell.Event`, so a tcell program feeds them into its own queue
and handles them in its one loop. `progressive.Place`, `Post`, `Remove` and
`Clear` build the sequences. A program compiled into the page and drawing
straight onto its terminal (xtcell) does not read its input, so it talks to
its widgets in its own process instead.

`cmd/ttydemo` is the example: its widget's button reaches the program as a
message, the program counts it in its cells and posts the count back, and
clicks on the widget are reported with their cell.

## Font

**Built**, page and local only. A program brings the font it is designed
for, as bytes: carried in it (Go: `//go:embed`), or fetched from where it
belongs (the store reads its site's `/font.css`).

    OSC 7337 ; font ; <data> ; <chunk> ST     (repeated while "more": true)
    OSC 7337 ; font ; reset ST

`<data>` is `{"family": "mononoki", "size": 15, "more": bool}` (size in CSS
pixels, optional), and the chunks are the font file, WOFF2, WOFF, TTF or
OTF, at most 4 MB. The host loads it with the FontFace API under a name
made from its content, so it never stands in for a page font of the same
name and a second run reuses it, and draws the terminal in it with its own
font behind for missing glyphs. The cells are measured again and the
terminal refit, which the program hears as an ordinary resize. When the
program exits, or sends `reset`, the host's font comes back; the size goes
back only if the program set one, so a zoom the person made stays. A font
that finishes loading after its program has ended is not used. Go:
`progressive.Font(family, size, data)`, `progressive.FontReset()`.

A remote program's font request is ignored: a font in a person's terminal is
theirs, and a program on another machine reaching for it is reaching for a
control in the wrong place. Discovery lists `font` only where it is allowed.

## The page

**Built** (title and address), page and local only.

| Request | Sequence |
|---|---|
| Title | standard OSC 0 / OSC 2: `document.title` while the program runs |
| Address | `OSC 7337 ; page ; <data> ST`, `{"path": "/p/A123", "title": "..."}` |
| Favicon | `OSC 7337 ; icon ; <data> ST`, `{"url": ...}` (planned) |

**The address.** The host puts the running command and the program's path
in the page's address, in the fragment —
`#run=<command line>&at=<path>` — which every host, a static one too, hands
back untouched, so what the program shows can be linked to, bookmarked and
shared. The title, if given, becomes the page's.

**A link.** When the page is opened by such an address, the host types the
command at the prompt and does **not** run it: a link that ran commands
would let any page reach this shell's files. The person presses Enter; the
program the command starts finds where the link pointed in Discovery's
reply, `"path"` (Go: `progressive.Current().LinkPath()`), and opens there.
It is handed once, to the command the link carried.

Title and address go back to what they were when the program exits. Go:
`progressive.Title`, `progressive.Page`. The store announces its pages as
the site's own paths (`/p/<part>`, `/cat/<category>`), so a link to a
product opens the store at that product.

Search engines do not run a page's programs, so an address alone does not
make a page indexed; a site that wants that serves the same paths as HTML
too, as magnetosphere.net does (Accessibility and search).

## Files, notifications, clipboard, sound

| What | Protocol | Status |
|---|---|---|
| Hyperlinks | OSC 8 | built (xterm-go) |
| Clipboard write | OSC 52 (`progressive.Copy`); reading is refused | built; remote asks the person |
| Downloads offered | iTerm2 OSC 1337 `File=` with `inline=0` (`progressive.Download`), one sequence, under 10 MB | built; remote asks the person |
| Notifications | OSC 9, OSC 777, kitty OSC 99 | planned |
| Inline images | iTerm2 OSC 1337 `File=` (inline=1), kitty graphics | planned |
| Files dropped onto the terminal | `OSC 7337 ; drop ; <data> ST` on input, the file written to the shell's filesystem | planned |
| Sound | `OSC 7337 ; sound ; <data> ST` (a URL or shipped bytes) | planned |
| Focus in/out | mode 1004 (`CSI I`, `CSI O`) | built |

## Accessibility and search

**Planned.** Cells are not semantic: a screen reader reads a grid of
characters, and a search engine sees nothing at all. A program can publish
what it shows as structure alongside the cells:

    OSC 7337 ; mirror ; <data> ST

`<data>` carries HTML (headings, lists, links, a table), which the host keeps
off-screen and live for assistive technology, and, for a page program, in
the document for indexing. This is how a TUI page meets the bar the web sets
for everything else; without it, it should not be anyone's only interface.

## Replies are not keys

A terminal's replies (DA, DSR, the reports above, the caps reply) are kept
apart from what the person types (xterm-go's `OnReply`). They go to the
command that is running, never to the line editor and never echoed; one the
command never reads is dropped when it ends, rather than reaching the next
command that reads stdin as though typed (`printf '\e[c'` used to leave
`ESC[?1;2c` for the next `read`). Typed keys carry over to the next command,
as type-ahead does in any terminal.

## Known gaps in websh's terminal

Found while writing this, to be fixed rather than worked around:

- No kitty keyboard protocol.
- Fixed: replies taken as typing, unanswered OSC 10/11 and CSI 14/16 t, CSI t
  reports switched off, focus events never sent, no XTVERSION.

## Prior art

None of this is without ancestors, and the design borrows from them.

- **DomTerm** (Per Bothner) inserts HTML into the terminal with OSC 72.
- **TermKit** (Steven Wittens, 2011) built a terminal on WebKit whose output
  was widgets.
- **iTerm2** (OSC 1337), **kitty** (graphics, keyboard, file transfer,
  notifications, text sizing, pointer shapes), **Terminology** and **sixel**
  put pictures and more into terminals.
- **Hyper**, **Extraterm** and **Wave** are terminals built on web engines.
- **NeWS** and **Plan 9's acme** made program output and interface one thing
  decades earlier.

What is new here is the combination: the program runs in the browser tab
itself, as wasm; the same Go binary is a plain TUI on a desktop; the program
supplies its own widgets rather than the host offering a fixed set; and the
whole channel is ordinary terminal output, so it composes with pipes, ssh,
mosh and skywire. OSC 7337 is not yet in a registry; the terminal working
group's proposal for one
(freedesktop terminal-wg/specifications#10) is where it should go.
