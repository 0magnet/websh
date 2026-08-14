//go:build js && wasm

// websh: a bash-like shell running entirely in the browser —
// github.com/0magnet/sh interpreting over an in-memory filesystem,
// displayed by an xterm-go terminal.
package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"syscall/js"

	"github.com/0magnet/afero"
	"github.com/0magnet/sh/v3/interp"
	xterm "github.com/0magnet/xterm-go"
	"github.com/0magnet/xterm-go/vt"

	"github.com/0magnet/websh/shell"
)

// termWriter converts LF to CRLF on the way to the terminal.
type termWriter struct{ term *xterm.Terminal }

func (w termWriter) Write(p []byte) (int, error) {
	w.term.WriteString(strings.ReplaceAll(string(p), "\n", "\r\n"))
	return len(p), nil
}

type session struct {
	term   *xterm.Terminal
	sh     *shell.Shell
	editor *shell.LineEditor

	running   bool
	rawInput  bool
	cancelRun context.CancelFunc
	stdinW    *io.PipeWriter

	lines chan string

	storage *idbStorage
	sigs    map[string]fileSig
}

func (s *session) prompt() string {
	if s.sh.Pending() {
		return "\x1b[1;33m>\x1b[0m "
	}
	dir := s.sh.Dir()
	if strings.HasPrefix(dir, "/home/user") {
		dir = "~" + dir[len("/home/user"):]
	}
	return "\x1b[1;32muser@websh\x1b[0m:\x1b[1;34m" + dir + "\x1b[0m$ "
}

func (s *session) writePrompt() {
	s.term.WriteString(s.prompt())
}

func main() {
	doc := js.Global().Get("document")
	container := doc.Call("getElementById", "terminal")

	opts := vt.NewOptions()
	opts.Scrollback = 2000
	term := xterm.New(opts)
	term.Open(container)
	term.Fit()
	if err := term.EnableWebGL(); err != nil {
		js.Global().Get("console").Call("log", "webgl unavailable, using DOM renderer: "+err.Error())
	}
	term.OnTitleChange = func(title string) { doc.Set("title", title) }

	resize := js.FuncOf(func(js.Value, []js.Value) any {
		term.Fit()
		return nil
	})
	js.Global().Call("addEventListener", "resize", resize)

	out := termWriter{term}
	stdinR, stdinW := io.Pipe()

	// restore the filesystem from IndexedDB (or seed a fresh one)
	vfs := afero.NewMemMapFs()
	storage, serr := openStorage()
	persisted := false
	if serr == nil {
		if n, err := storage.loadAll(vfs); err == nil {
			persisted = true
			if n == 0 {
				if err := shell.Seed(vfs); err != nil {
					term.WriteString("failed to seed filesystem: " + err.Error() + "\r\n")
				}
			}
		}
	}
	if !persisted {
		if err := shell.Seed(vfs); err != nil {
			term.WriteString("failed to seed filesystem: " + err.Error() + "\r\n")
		}
	}

	sh, err := shell.New(vfs, stdinR, out, out)
	if err != nil {
		term.WriteString("failed to start shell: " + err.Error() + "\r\n")
		select {}
	}

	registerBrowserApplets()
	s := &session{term: term, sh: sh, stdinW: stdinW, lines: make(chan string, 8)}
	if persisted {
		s.storage = storage
		shell.RegisterApplet("reset-fs", "wipe the persisted filesystem and reload", func(ctx context.Context, _ *shell.Shell, hc *interp.HandlerContext, args []string) int {
			if err := storage.clear(); err != nil {
				shell.Printf(hc.Stderr, "reset-fs: %v\n", err)
				return 1
			}
			shell.Println(hc.Stdout, "persisted filesystem cleared; reloading...")
			js.Global().Get("location").Call("reload")
			return 0
		})
		s.sigs = storage.syncFS(vfs, nil) // baseline snapshot (also persists the seed)
	}
	// include the browser applets (and reset-fs) in /bin
	if err := sh.PopulateBin(); err != nil {
		term.WriteString("failed to populate /bin: " + err.Error() + "\r\n")
	}

	s.editor = &shell.LineEditor{
		Echo: func(str string) { term.WriteString(str) },
		Redraw: func(content string, back int) {
			out := "\r\x1b[2K" + s.prompt() + content
			if back > 0 {
				out += fmt.Sprintf("\x1b[%dD", back)
			}
			term.WriteString(out)
		},
		Submit: func(line string) { s.lines <- line },
		Interrupt: func() {
			s.sh.CancelPending()
			s.writePrompt()
		},
		EOF: func() {
			term.WriteString("logout? this shell has nowhere else to go :)\r\n")
			s.writePrompt()
		},
		ClearScreen: func() {
			term.WriteString("\x1b[2J\x1b[H")
			s.writePrompt()
			term.WriteString(s.editor.Line())
		},
		Complete: func(word string, isFirstWord bool) []string {
			if isFirstWord && !strings.Contains(word, "/") {
				var out []string
				for _, name := range append(shell.AppletNames(), shellBuiltins...) {
					if strings.HasPrefix(name, word) {
						out = append(out, name)
					}
				}
				sort.Strings(out)
				return out
			}
			// path completion against the virtual filesystem
			dir, base := filepath.Split(word)
			searchDir := dir
			if !filepath.IsAbs(searchDir) {
				searchDir = filepath.Join(sh.Dir(), dir)
			}
			infos, err := afero.ReadDir(sh.FS, filepath.Clean(searchDir))
			if err != nil {
				return nil
			}
			var out []string
			for _, info := range infos {
				if !strings.HasPrefix(info.Name(), base) {
					continue
				}
				cand := dir + info.Name()
				if info.IsDir() {
					cand += "/"
				}
				out = append(out, cand)
			}
			sort.Strings(out)
			return out
		},
	}

	// the history builtin reads the line editor's list
	if err := sh.UseHistory(s.editor.History, s.editor.ClearHistory); err != nil {
		term.WriteString("history unavailable: " + err.Error() + "\r\n")
	}

	// full-screen applets (less) take raw input and know the size
	sh.RawMode = func(on bool) { s.rawInput = on }
	sh.Size = func() (int, int) { return term.Core.Cols(), term.Core.Rows() }

	term.Core.OnData = func(data string) {
		if s.running {
			if s.rawInput {
				// a full-screen applet owns the terminal: raw bytes,
				// no echo; Ctrl+C is passed through for it to handle
				go shell.Write(s.stdinW, []byte(data))
				return
			}
			// a command is reading stdin: pass input through (with
			// echo and CRLF translation), Ctrl+C cancels
			if strings.Contains(data, "\x03") {
				if s.cancelRun != nil {
					s.cancelRun()
				}
				return
			}
			echo := strings.ReplaceAll(data, "\r", "\r\n")
			term.WriteString(echo)
			go shell.Write(s.stdinW, []byte(strings.ReplaceAll(data, "\r", "\n")))
			return
		}
		s.editor.Input(data)
	}

	// the shell runs on its own goroutine so the JS event loop (and
	// therefore the terminal) stays responsive while commands run
	go s.run()

	term.WriteString("\x1b[1;36mwebsh\x1b[0m — bash in your browser · \x1b[2mgithub.com/0magnet/websh\x1b[0m\r\n")
	fsNote := "an in-memory filesystem (IndexedDB unavailable — changes are not persisted)"
	if persisted {
		fsNote = "a filesystem persisted in IndexedDB — your files survive reloads (\x1b[1mreset-fs\x1b[0m to wipe)"
	}
	term.WriteString("the shell is \x1b[1m0magnet/sh\x1b[0m (mvdan/sh fork) on " + fsNote + "\r\n")
	term.WriteString("try: \x1b[1mhelp\x1b[0m · \x1b[1mcat readme.md\x1b[0m · \x1b[1msource demo.sh\x1b[0m · \x1b[1mtree /\x1b[0m\r\n\r\n")
	s.writePrompt()

	select {}
}

func (s *session) run() {
	for line := range s.lines {
		if !s.sh.Pending() {
			s.editor.AddHistory(line)
		}

		// Canceling this at the end of the line is safe: the interpreter
		// detaches background jobs from it, so `sleep 30 &` survives to the
		// next prompt as it would in bash.
		ctx, cancel := context.WithCancel(context.Background())
		s.cancelRun = cancel
		s.running = true

		needMore, err := s.sh.Run(ctx, line)

		s.running = false
		s.cancelRun = nil
		cancel()

		if err != nil {
			msg := err.Error()
			if !strings.HasPrefix(msg, "exit status") {
				s.term.WriteString("websh: " + strings.ReplaceAll(msg, "\n", "\r\n") + "\r\n")
			}
		}
		_ = needMore
		if s.storage != nil {
			s.sigs = s.storage.syncFS(s.sh.FS, s.sigs)
		}
		s.writePrompt()
	}
}

// shellBuiltins are the interpreter builtins offered by completion.
var shellBuiltins = []string{
	"cd", "pwd", "echo", "printf", "read", "exit", "export", "unset",
	"source", "test", "true", "false", "set", "shift", "local",
	"declare", "eval", "alias", "unalias", "type", "return", "break",
	"continue", "pushd", "popd", "dirs", "let", "getopts", "wait",
	"jobs", "kill", "disown", "fg", "bg", "enable", "compgen", "history",
	"builtin", "umask", "times", "trap", "shopt", "mapfile", "readarray",
}
