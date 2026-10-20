//go:build !js && !windows

package commands

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/0magnet/websh/shell/pty"
	"github.com/0magnet/websh/term"
)

var serveAddr string

var serveCmd = &cobra.Command{
	Use:   "serve [-- COMMAND [ARG...]]",
	Short: "serve this machine's shell to a browser",
	Long: "serve opens this machine's shell in a browser: websh's terminal page at /\n" +
		"and a terminal running COMMAND, or $SHELL, for each page that opens it.\n" +
		"It listens on loopback by default. Anyone who can reach the address gets\n" +
		"the shell, so listen elsewhere only behind something that checks who is\n" +
		"asking. Pages on other sites are refused.",
	Args:         cobra.ArbitraryArgs,
	SilenceUsage: true,
	RunE: func(_ *cobra.Command, args []string) error {
		if len(args) == 0 {
			sh := os.Getenv("SHELL")
			if sh == "" {
				sh = "/bin/sh"
			}
			args = []string{sh}
		}
		mux := http.NewServeMux()
		mux.Handle("/pty", pty.Handler{Command: func(*http.Request) *exec.Cmd {
			c := exec.Command(args[0], args[1:]...) //nolint:gosec // the operator's own command
			c.Env = append(os.Environ(), "TERM=xterm-256color")
			return c
		}})
		host, _ := os.Hostname() //nolint:errcheck // the prompt shows websh without it
		mux.Handle("/", term.Handler(func(*http.Request) term.Page {
			return term.Page{Title: "websh " + host, Host: host, Run: "pty pty"}
		}))
		fmt.Fprintf(os.Stderr, "websh: serving %s at http://%s/\n", args[0], serveAddr)
		srv := &http.Server{Addr: serveAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		return srv.ListenAndServe()
	},
}

func init() {
	serveCmd.Flags().StringVarP(&serveAddr, "addr", "a", "127.0.0.1:7681", "address to listen on")
	RootCmd.AddCommand(serveCmd)
}
