// Package commands is the websh command: the native tools of this repository
// as subcommands. The browser programs (cmd/websh, cmd/ttydemo,
// cmd/wasmwidget) are js/wasm mains and are built by build.sh.
package commands

import (
	"os"

	"github.com/0magnet/calvin/clihelp"
	"github.com/spf13/cobra"

	"github.com/0magnet/websh/progcheck"
	"github.com/0magnet/websh/progdemo"
)

// RootCmd is the websh command.
var RootCmd = &cobra.Command{
	Use:   "websh",
	Short: "tools for websh, bash in the browser, and the progressive terminal",
	RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
}

func init() {
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

// Execute runs the websh command.
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
