// Package commands is the websh command: websh itself on this machine's
// terminal, and the native tools of this repository as subcommands. The
// browser programs (cmd/websh, cmd/ttydemo, cmd/wasmwidget) are js/wasm mains
// and are built by build.sh.
package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/0magnet/calvin/clihelp"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/0magnet/websh/console"
	"github.com/0magnet/websh/progcheck"
	"github.com/0magnet/websh/progdemo"
)

var command string

// RootCmd is the websh command.
var RootCmd = &cobra.Command{
	Use:   "websh [FILE [ARG...]]",
	Short: "bash in the browser, here on this machine's terminal",
	Long: "websh with no arguments is an interactive shell on this terminal: the\n" +
		"shell, applets and line editor of websh in a page, on this machine's files\n" +
		"and programs. With -c it runs a command line, with FILE a script, and with\n" +
		"its input not a terminal, the script it reads.",
	Args:          cobra.ArbitraryArgs,
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(c *cobra.Command, args []string) error {
		var opt console.Options
		var code int
		var err error
		switch {
		case c.Flags().Changed("command"):
			code, err = console.Script(strings.NewReader(command), "websh", args, opt)
		case len(args) > 0:
			f, ferr := os.Open(args[0])
			if ferr != nil {
				return exitError{127, ferr}
			}
			defer f.Close() //nolint:errcheck // read-only
			code, err = console.Script(f, args[0], args[1:], opt)
		case term.IsTerminal(int(os.Stdin.Fd())):
			code, err = console.Run(opt)
		default:
			code, err = console.Script(os.Stdin, "websh", nil, opt)
		}
		if err != nil || code != 0 {
			return exitError{code, err}
		}
		return nil
	},
}

// exitError carries the shell's exit status out to Execute.
type exitError struct {
	code int
	err  error
}

func (e exitError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("exit status %d", e.code)
}

func init() {
	RootCmd.Flags().StringVarP(&command, "command", "c", "", "run this command line, then exit")
	// A terminal starts "the user's shell" as a login shell (progterm, sshd,
	// login). websh reads no profile, so it is accepted and means nothing.
	RootCmd.Flags().BoolP("login", "l", false, "accepted for login shells; websh reads no profile")
	// As in bash, flags end at the script: what follows it is its own.
	RootCmd.Flags().SetInterspersed(false)
	RootCmd.AddCommand(
		&cobra.Command{
			Use:   "progdemo",
			Short: "show what a progressive terminal program can do",
			Long: "progdemo shows what a progressive terminal program can do, each thing\n" +
				"beside what the same program does where the host cannot. It asks its\n" +
				"terminal what it offers and shows six pages. Run it in a desktop\n" +
				"terminal, or over ssh into websh. Tab or 1-6 change page, q quits.",
			Args:         cobra.NoArgs,
			SilenceUsage: true,
			RunE:         func(*cobra.Command, []string) error { return progdemo.Run() },
		},
		&cobra.Command{
			Use:   "progcheck",
			Short: "report what the terminal it runs in can do",
			Long: "progcheck reports whether the terminal is a progressive terminal host and\n" +
				"what it offers (PROTOCOL.md), and the standard and kitty extensions a\n" +
				"program may use: pictures, keyboard, sizes, colors, modes.",
			Args:         cobra.NoArgs,
			SilenceUsage: true,
			RunE:         func(*cobra.Command, []string) error { return progcheck.Main() },
		},
	)
	clihelp.Init(RootCmd, "websh")
}

// Execute runs the websh command, and exits with the shell's status.
func Execute() {
	err := RootCmd.Execute()
	if err == nil {
		return
	}
	var ee exitError
	if errors.As(err, &ee) {
		if ee.err != nil {
			fmt.Fprintln(os.Stderr, "websh:", ee.err)
		}
		if ee.code == 0 {
			ee.code = 1
		}
		os.Exit(ee.code)
	}
	fmt.Fprintln(os.Stderr, "websh:", err)
	os.Exit(1)
}
