# websh

A bash-like shell running entirely in your browser — no server, no container, no emulator. WebAssembly all the way down.

**[Live demo](https://0magnet.github.io/websh/)** (TinyGo build, 3.3 MB — the default) · [standard Go build](https://0magnet.github.io/websh/go/) (12 MB)

```
user@websh:~$ for i in $(seq 3); do echo line $i; done | grep 2
line 2
user@websh:~$ echo it persists > keep.txt   # survives page reloads
```

## What it is

Three Go libraries composed into one wasm binary:

- **[0magnet/xterm-go](https://github.com/0magnet/xterm-go)** — the terminal (a Go port of xterm.js 6.0.0), rendering with its WebGL renderer.
- **[0magnet/sh](https://github.com/0magnet/sh)** — the shell language (a fork of [mvdan/sh](https://github.com/mvdan/sh)), a real Bash/POSIX interpreter: pipes, redirections, globbing, functions, heredocs, command substitution, arithmetic, control flow.
- **[0magnet/u-root](https://github.com/0magnet/u-root)** — pure-Go userland utilities (currently `pkg/ls` for long-listing format; more as the fork grows js support).

Plus **[0magnet/afero](https://github.com/0magnet/afero)** (an [afero](https://github.com/spf13/afero) fork) providing the filesystem: an in-memory fs wired into the interpreter's open/stat/readdir/access handlers, **persisted to IndexedDB** — your files survive page reloads (`reset-fs` wipes).

## Why forks

The interpreter, userland and filesystem run in an environment upstream doesn't target: no OS pipes, no subprocesses, no `Getwd`, and TinyGo's incomplete `os`/`syscall`. The forks carry those changes without being gated on upstream review — `0magnet/sh` abstracts the runner's stdin so pipelines and heredocs use in-process `io.Pipe` on js/wasm, `0magnet/u-root` has js/tinygo build variants for `pkg/ls`, and `0magnet/afero` drops the `net/http` dependency and shims the `os` functions TinyGo lacks.

## What works

- The shell language: `if`/`for`/`while`/`case`, functions, `$(...)`, `$(( ))`, globs, heredocs, `&&`/`||`, multi-line input with continuation prompts, `source`
- Pipes and redirections against the virtual filesystem
- **`awk`** ([goawk](https://github.com/benhoyt/goawk)) and **`jq`** ([gojq](https://github.com/itchyny/gojq)) — the real things, as pure-Go libraries
- ~45 applets: `ls cat mkdir rm cp mv touch head tail wc grep sed find cut tr xargs tac nl seq sort uniq tree du stat chmod md5sum sha256sum base64 xxd basename dirname date sleep clear env which uname hostname help reset-fs` + the interpreter's builtins (`cd pwd echo printf read test exit export unset alias eval pushd popd ...`)
- **A text editor**: `edit file` — full-screen, in the spirit of [skywire](https://github.com/skycoin/skywire)'s femto-based `edit` command (Ctrl+S save, Ctrl+Q quit, Ctrl+K cut)
- **A pager**: `less`/`more` (space/b page, j/k line, g/G ends, q quits)
- **The browser console, in the shell**: `js 'document.title'` evaluates JavaScript in the page (promises awaited, objects printed as JSON) and `logs` reads captured `console.*` output — `-f` to follow, `-e` for errors only, `-n N`, `-c` to clear. Pipe it: `logs -e -p | wc -l`
- **Browser superpowers**: `curl` (fetch, CORS applies), `nc` (WebSocket netcat), `download` (vfs file → your Downloads), `upload` (file picker → vfs), `pbcopy`/`pbpaste` (system clipboard)
- Line editing: **tab completion** (commands and paths), history (↑/↓), cursor movement (←/→, Ctrl+A/E), kill (Ctrl+U/K/W), Ctrl+C cancels running commands (`sleep 30` → `^C`), Ctrl+L clears

```
user@websh:~$ curl -s https://api.github.com/repos/0magnet/websh | jq -r .description
bash in your browser: xterm-go + a Go shell interpreter + IndexedDB filesystem, all in WebAssembly
```
- Persistence: the filesystem diffs+flushes to IndexedDB after every command

## Architecture

```
cmd/websh/       js/wasm entry: terminal wiring, prompt, IndexedDB persistence
shell/           pure Go, natively testable (go test ./shell/):
  shell.go         interp.Runner + afero handlers (open/stat/readdir/access/exec)
  applets.go       the userland, written against afero
  editor.go        readline-style line discipline (escape-seq parser, history)
shell/browser/   js/wasm applets any embedder can turn on with browser.Register():
  browser.go       js, download, upload, curl, nc, pbcopy, pbpaste
  console.go       console.* capture behind `logs` (chains with a host page's
                   own capture, and backfills from it)
```

Embedders get the browser applets by importing one package:

```go
import "github.com/0magnet/websh/shell/browser"

browser.Register()   // before sh.PopulateBin(), so they appear in /bin
```

The `shell` package has no `syscall/js` — the whole engine (interpreter, filesystem, applets, line editor) runs and is tested natively. The wasm layer is only terminal glue.

## Building

```bash
# TinyGo (the default live demo — ~2 MB):
tinygo build -target wasm -no-debug -o docs/main.wasm ./cmd/websh
# standard Go (served at /go/ — ~8 MB):
GOOS=js GOARCH=wasm go build -o docs/go/main.wasm ./cmd/websh
```

Both toolchains are supported and both are deployed ([TinyGo](https://0magnet.github.io/websh/), [standard Go](https://0magnet.github.io/websh/go/)); the forks carry the TinyGo compatibility shims. Use the matching `wasm_exec.js` for whichever compiled the binary.

## Roadmap

- More u-root applets as the fork gains js/wasm build support
- Literal femto/tview support would need a rename-chain of forks (tcell's tty screen is excluded from js builds); the current `edit` keeps femto's keybindings without the dependency chain

## License

MIT (websh). The forks retain their upstream licenses: sh (BSD-3-Clause), u-root (BSD-3-Clause), afero (Apache-2.0), xterm-go (MIT).
