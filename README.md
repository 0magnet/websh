# websh

A bash-like shell running entirely in your browser — no server, no container, no emulator. WebAssembly all the way down.

**[Live demo](https://0magnet.github.io/websh/)**

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

Plus [afero](https://github.com/spf13/afero) providing the filesystem: an in-memory fs wired into the interpreter's open/stat/readdir/access handlers, **persisted to IndexedDB** — your files survive page reloads (`reset-fs` wipes).

## Why forks

The interpreter and userland run in an environment upstream doesn't target: no OS pipes, no subprocesses, no `Getwd`, 32-bit ints under TinyGo. The forks carry those changes without being gated on upstream review — e.g. `0magnet/sh` abstracts the runner's stdin so pipelines and heredocs use in-process `io.Pipe` on js/wasm, and `0magnet/u-root` has js build variants for `pkg/ls`.

## What works

- The shell language: `if`/`for`/`while`/`case`, functions, `$(...)`, `$(( ))`, globs, heredocs, `&&`/`||`, multi-line input with continuation prompts, `source`
- Pipes and redirections against the virtual filesystem
- ~26 applets: `ls cat mkdir rm cp mv touch head tail wc grep seq sort uniq tree basename dirname date sleep clear env which uname hostname help reset-fs` + the interpreter's builtins (`cd pwd echo printf read test exit export unset ...`)
- Line editing: history (↑/↓), cursor movement (←/→, Ctrl+A/E), kill (Ctrl+U/K/W), Ctrl+C cancels running commands (`sleep 30` → `^C`), Ctrl+L clears
- Persistence: the filesystem diffs+flushes to IndexedDB after every command

## Architecture

```
cmd/websh/   js/wasm entry: terminal wiring, prompt, IndexedDB persistence
shell/       pure Go, natively testable (go test ./shell/):
  shell.go     interp.Runner + afero handlers (open/stat/readdir/access/exec)
  applets.go   the userland, written against afero
  editor.go    readline-style line discipline (escape-seq parser, history)
```

The `shell` package has no `syscall/js` — the whole engine (interpreter, filesystem, applets, line editor) runs and is tested natively. The wasm layer is only terminal glue.

## Building

```bash
GOOS=js GOARCH=wasm go build -o docs/main.wasm ./cmd/websh
```

TinyGo doesn't build the interpreter yet (reflection); a reflection-free interp is on the fork's roadmap.

## Roadmap

- More u-root applets as the fork gains js/wasm build support
- Networking applets (`curl`, `nc`) — in the browser directly, and via the [skywire wasm visor](https://github.com/skycoin/skywire) for pty/network reach beyond the page origin
- TinyGo compatibility (reflection-free interpreter fork)

## License

MIT (websh). The forks retain their upstream licenses: sh (BSD-3-Clause), u-root (BSD-3-Clause), xterm-go (MIT).
