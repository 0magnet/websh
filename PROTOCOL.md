# The websh host protocol

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

**Built:** only page and local exist so far; there is no remote gating yet,
and ssh and mosh output is treated as local. **Next:** the session marks a
remote command's output (ssh, mosh) as remote while it runs.

## Discovery

**Next.**

    query:  OSC 7337 ; caps ? ST
    reply:  OSC 7337 ; caps ; <data> ST

```json
{
  "v": 1,
  "host": "websh",
  "version": "<module version>",
  "trust": "local",
  "cell": { "w": 8.99, "h": 19.71 },
  "dpr": 1.0156,
  "features": ["place", "place.input", "widget.offer", "event", "font", "page", "notify"]
}
```

`cell` is the cell size in CSS pixels, `dpr` the device pixel ratio: what a
program needs to draw a picture that fits its cells exactly (a half-block
image, say, whose pixels are half a cell tall and not square).

A program that does not know whether it is in websh at all sends the query
and then DA1 (`CSI c`), which every terminal answers, and reads its input
until the DA1 reply arrives. If the caps reply came first, the host speaks
this protocol; if only DA1 came, it does not. No timeout is needed, and
nothing is guessed. The Go package `hybrid` does exactly this.

Standard queries a program may also use, answered by websh's terminal
(xterm-go):

| Query | Reply | Status |
|---|---|---|
| DA1 `CSI c` | `CSI ? 1 ; 2 c` | built |
| DA2 `CSI > c` | `CSI > 0 ; 276 ; 0 c` | built |
| XTVERSION `CSI > q` | `DCS > \| websh(<version>) ST` | next |
| Cell size `CSI 16 t` | `CSI 6 ; <h> ; <w> t` (device pixels) | next |
| Text area `CSI 14 t` | `CSI 4 ; <h> ; <w> t` | next |
| Size in cells `CSI 18 t` | `CSI 8 ; <rows> ; <cols> t` | built, but switched off; next: on |
| Colors OSC 10/11 `?` | `OSC 10 ; rgb:rrrr/gggg/bbbb ST` | next |
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

The program keeps drawing its cells under every placement; that is what a
terminal without placements shows. Placements go when the command ends.

### Widgets a program offers

**Built** for page and local programs. A program running in the tab offers a
widget by writing it into a plain registry on the page,
`globalThis.webshWidgets[name] = {mount, owner}` (Go: `widget.Register`), and
places it by name. websh calls `mount(el)` on a microtask of its own, never
from its own stack: the program is a separate Go runtime, and two on one
stack corrupt each other. What a program offered is withdrawn when it exits.
`widget.Shown()` tells a program its terminal shows placements (today from
`WEBSH_PLACEMENTS=1`; next, from Discovery).

### Shipped widgets

**Planned.** A program on another machine cannot offer a function, but it can
send a widget as content:

    OSC 7337 ; ship ; <name> ; <data> [; <chunk>] ST

`<data>` is `{"kind": "html" | "wasm", "more": bool}`; the chunks are the
widget itself (an HTML document, or a wasm module with its loader). The host
runs it in an `<iframe sandbox="allow-scripts">` with no same-origin access:
it can draw and compute, and it can talk only to the program, through the
host, by `postMessage`. Placing it by name works as for any widget. This is
what makes a hybrid program work over ssh: the store's globe and card form,
drawn on the server's behalf in the person's tab.

## Events

**Planned.** A placement made with `"events": true` reports what happens to it
as input, as a terminal reports mouse clicks:

    OSC 7337 ; event ; <id> ; <data> ST

`<data>` is `{"type": "click" | "dblclick" | "message" | ..., ...}`. A widget
(offered or shipped) posts its own events — "payment succeeded", "file
chosen" — and the program reads them in its ordinary input loop. Going the
other way, the program sends a message to a widget:

    OSC 7337 ; post ; <id> ; <data> ST

Programs that never asked for events never see one.

## Font

**Planned**, page and local only. A program brings the font it is designed
for, as bytes it carries (Go: `//go:embed`):

    OSC 7337 ; font ; <data> ; <chunk> ST     (repeated, "more": true)
    OSC 7337 ; font ; reset ST

`<data>` is `{"family": "mononoki", "size": 15, "format": "woff2", "more":
bool}`. The host loads it with the FontFace API, switches the terminal to it,
measures the new cell size and resizes the program's terminal, which the
program hears as an ordinary resize. The host caches fonts by content for the
session, and puts its own font back when the program exits.

A remote program's font request is ignored: a font in a person's terminal is
theirs, and a program on another machine reaching for it is reaching for a
control in the wrong place.

## The page

**Planned**, page and local only.

| Request | Sequence |
|---|---|
| Title | standard OSC 0 / OSC 2 (sets `document.title` while the program runs) |
| Address | `OSC 7337 ; page ; <data> ST`, `{"path": "/p/A123", "title": "..."}`: `history.replaceState`, so what the program shows can be linked to and opened again |
| Favicon | `OSC 7337 ; icon ; <data> ST`, `{"url": ...}` |

An address the program sets is what a link to the page opens: the page hands
it to the program at start (the program reads its first path from Discovery's
reply, `"path"`). A TUI page becomes something that can be shared, bookmarked
and indexed.

## Files, notifications, clipboard, sound

| What | Protocol | Status |
|---|---|---|
| Hyperlinks | OSC 8 | built (xterm-go) |
| Clipboard write | OSC 52 | planned |
| Notifications | OSC 9, OSC 777, kitty OSC 99 | planned |
| Inline images | iTerm2 OSC 1337 `File=` (inline=1), kitty graphics | planned |
| Downloads offered | iTerm2 OSC 1337 `File=` (inline=0) | planned |
| Files dropped onto the terminal | `OSC 7337 ; drop ; <data> ST` on input, the file written to the shell's filesystem | planned |
| Sound | `OSC 7337 ; sound ; <data> ST` (a URL or shipped bytes) | planned |
| Focus in/out | mode 1004 (`CSI I`, `CSI O`) | next (xterm-go sets the mode but sends nothing) |

## Accessibility and search

**Planned.** Cells are not semantic: a screen reader reads a grid of
characters, and a search engine sees nothing at all. A program can publish
what it shows as structure alongside the cells:

    OSC 7337 ; mirror ; <data> ST

`<data>` carries HTML (headings, lists, links, a table), which the host keeps
off-screen and live for assistive technology, and, for a page program, in
the document for indexing. This is how a TUI page meets the bar the web sets
for everything else; without it, it should not be anyone's only interface.

## Known gaps in websh's terminal

Found while writing this, to be fixed rather than worked around:

- Terminal replies (DA, DSR, ...) reach the shell as if typed: at a prompt they
  land in the line editor, and in cooked mode they are echoed. xterm-go knows
  which bytes are replies and drops that before the session sees them.
- OSC 10/11 queries are parsed and never answered; CSI 14/16 t likewise; all
  CSI t reports are off.
- Mode 1004 focus events are never sent.
- OSC 0/2 titles are parsed and go nowhere.
- No XTVERSION, no OSC 52, no kitty keyboard protocol.

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
