//go:build js && wasm

package main

// Browser-only applets: things a normal shell cannot do. Registered
// from main so the pure-Go shell package stays js-free.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"syscall/js"

	"github.com/0magnet/afero"
	"github.com/0magnet/sh/v3/interp"

	"github.com/0magnet/websh/shell"
)

// awaitPromise blocks the calling goroutine until a JS Promise
// settles.
func awaitPromise(p js.Value) (js.Value, error) {
	done := make(chan struct{})
	var result js.Value
	var err error
	onOK := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			result = args[0]
		}
		close(done)
		return nil
	})
	onErr := js.FuncOf(func(_ js.Value, args []js.Value) any {
		msg := "promise rejected"
		if len(args) > 0 {
			m := args[0]
			if m.Type() == js.TypeObject && !m.Get("message").IsUndefined() {
				msg = m.Get("message").String()
			} else {
				msg = m.String()
			}
		}
		err = errors.New(msg)
		close(done)
		return nil
	})
	defer onOK.Release()
	defer onErr.Release()
	p.Call("then", onOK).Call("catch", onErr)
	<-done
	return result, err
}

func registerBrowserApplets() {
	shell.RegisterApplet("download", "save a file from the shell to your Downloads", runDownload)
	shell.RegisterApplet("upload", "pick a local file and copy it into the shell", runUpload)
	shell.RegisterApplet("curl", "fetch a URL (-o file; CORS applies)", runCurl)
	shell.RegisterApplet("pbcopy", "copy stdin to the system clipboard", runPbcopy)
	shell.RegisterApplet("pbpaste", "paste the system clipboard to stdout", runPbpaste)
}

func resolveIn(hc *interp.HandlerContext, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(hc.Dir, p)
}

func runDownload(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(hc.Stderr, "usage: download <file>")
		return 1
	}
	data, err := afero.ReadFile(s.FS, resolveIn(hc, args[0]))
	if err != nil {
		fmt.Fprintf(hc.Stderr, "download: %v\n", err)
		return 1
	}
	u8 := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(u8, data)
	parts := js.Global().Get("Array").New()
	parts.Call("push", u8)
	blob := js.Global().Get("Blob").New(parts)
	url := js.Global().Get("URL").Call("createObjectURL", blob)
	a := js.Global().Get("document").Call("createElement", "a")
	a.Set("href", url)
	a.Set("download", filepath.Base(args[0]))
	a.Call("click")
	js.Global().Get("URL").Call("revokeObjectURL", url)
	fmt.Fprintf(hc.Stdout, "downloading %s (%d bytes)\n", filepath.Base(args[0]), len(data))
	return 0
}

func runUpload(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
	doc := js.Global().Get("document")
	input := doc.Call("createElement", "input")
	input.Set("type", "file")

	picked := make(chan js.Value, 1)
	onChange := js.FuncOf(func(js.Value, []js.Value) any {
		files := input.Get("files")
		if files.Get("length").Int() > 0 {
			picked <- files.Index(0)
		} else {
			picked <- js.Null()
		}
		return nil
	})
	defer onChange.Release()
	input.Set("onchange", onChange)
	input.Call("click")
	fmt.Fprintln(hc.Stdout, "waiting for file selection... (Ctrl+C to cancel)")

	var file js.Value
	select {
	case file = <-picked:
	case <-ctx.Done():
		fmt.Fprintln(hc.Stderr, "upload: cancelled")
		return 130
	}
	if file.IsNull() {
		fmt.Fprintln(hc.Stderr, "upload: no file selected")
		return 1
	}
	buf, err := awaitPromise(file.Call("arrayBuffer"))
	if err != nil {
		fmt.Fprintf(hc.Stderr, "upload: %v\n", err)
		return 1
	}
	u8 := js.Global().Get("Uint8Array").New(buf)
	data := make([]byte, u8.Get("length").Int())
	js.CopyBytesToGo(data, u8)

	name := file.Get("name").String()
	dest := resolveIn(hc, name)
	if len(args) > 0 {
		dest = resolveIn(hc, args[0])
		if info, err := s.FS.Stat(dest); err == nil && info.IsDir() {
			dest = filepath.Join(dest, name)
		}
	}
	if err := afero.WriteFile(s.FS, dest, data, 0o644); err != nil {
		fmt.Fprintf(hc.Stderr, "upload: %v\n", err)
		return 1
	}
	fmt.Fprintf(hc.Stdout, "%s (%d bytes)\n", dest, len(data))
	return 0
}

func runCurl(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
	outFile := ""
	var urlArg string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-o" && i+1 < len(args):
			i++
			outFile = args[i]
		case args[i] == "-s" || args[i] == "-L":
			// accepted for muscle-memory compatibility; fetch always
			// follows redirects and there is no progress meter anyway
		default:
			urlArg = args[i]
		}
	}
	if urlArg == "" {
		fmt.Fprintln(hc.Stderr, "usage: curl [-o file] <url>   (subject to CORS)")
		return 1
	}
	if !strings.Contains(urlArg, "://") {
		urlArg = "https://" + urlArg
	}
	resp, err := awaitPromise(js.Global().Call("fetch", urlArg))
	if err != nil {
		fmt.Fprintf(hc.Stderr, "curl: %v (cross-origin requests need CORS headers)\n", err)
		return 1
	}
	if !resp.Get("ok").Bool() {
		fmt.Fprintf(hc.Stderr, "curl: HTTP %d\n", resp.Get("status").Int())
		return 22
	}
	buf, err := awaitPromise(resp.Call("arrayBuffer"))
	if err != nil {
		fmt.Fprintf(hc.Stderr, "curl: %v\n", err)
		return 1
	}
	u8 := js.Global().Get("Uint8Array").New(buf)
	data := make([]byte, u8.Get("length").Int())
	js.CopyBytesToGo(data, u8)
	if outFile != "" {
		if err := afero.WriteFile(s.FS, resolveIn(hc, outFile), data, 0o644); err != nil {
			fmt.Fprintf(hc.Stderr, "curl: %v\n", err)
			return 1
		}
		fmt.Fprintf(hc.Stdout, "saved %d bytes to %s\n", len(data), outFile)
		return 0
	}
	hc.Stdout.Write(data)
	return 0
}

func runPbcopy(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
	data, err := io.ReadAll(hc.Stdin)
	if err != nil {
		fmt.Fprintf(hc.Stderr, "pbcopy: %v\n", err)
		return 1
	}
	clip := js.Global().Get("navigator").Get("clipboard")
	if clip.IsUndefined() {
		fmt.Fprintln(hc.Stderr, "pbcopy: clipboard API unavailable")
		return 1
	}
	if _, err := awaitPromise(clip.Call("writeText", string(data))); err != nil {
		fmt.Fprintf(hc.Stderr, "pbcopy: %v\n", err)
		return 1
	}
	return 0
}

func runPbpaste(ctx context.Context, s *shell.Shell, hc *interp.HandlerContext, args []string) int {
	clip := js.Global().Get("navigator").Get("clipboard")
	if clip.IsUndefined() {
		fmt.Fprintln(hc.Stderr, "pbpaste: clipboard API unavailable")
		return 1
	}
	text, err := awaitPromise(clip.Call("readText"))
	if err != nil {
		fmt.Fprintf(hc.Stderr, "pbpaste: %v\n", err)
		return 1
	}
	fmt.Fprint(hc.Stdout, text.String())
	return 0
}
