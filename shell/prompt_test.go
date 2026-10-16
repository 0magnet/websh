package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/0magnet/afero"
)

func TestSourceRCPersists(t *testing.T) {
	var out strings.Builder
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	rc := "alias hi='echo hello'\n" +
		"greet() { echo \"greet $1\"; }\n" +
		"export WHO=world\n" +
		"cd /tmp\n"
	if err := afero.WriteFile(sh.FS, "/home/user/.webshrc", []byte(rc), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := sh.SourceRC(ctx); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"hi", "greet $WHO", "pwd"} {
		if _, err := sh.Run(ctx, line); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
	}
	if got := out.String(); got != "hello\ngreet world\n/tmp\n" {
		t.Fatalf("output %q", got)
	}
	if _, ok := sh.PS1(); ok {
		t.Fatal("an rc that does not set PS1 changed the prompt")
	}
}

func TestSourceRCMissing(t *testing.T) {
	sh, _ := feedShell(t, "")
	if err := sh.SourceRC(context.Background()); err != nil {
		t.Fatalf("missing rc: %v", err)
	}
}

func TestSourceRCExit(t *testing.T) {
	sh, _ := feedShell(t, "")
	if err := afero.WriteFile(sh.FS, "/home/user/.webshrc", []byte("x=1\nexit 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = sh.SourceRC(context.Background()) //nolint:errcheck // the exit status
	if _, err := sh.Run(context.Background(), "true"); err != nil || sh.Exited() {
		t.Fatalf("shell unusable after an rc exit: %v", err)
	}
}

func TestPS1Escapes(t *testing.T) {
	sh, _ := feedShell(t, "", "cd /tmp", `USER=ann HOSTNAME=box.example; WHERE=here`)
	if _, ok := sh.PS1(); ok {
		t.Fatal("default PS1 reported as set")
	}
	cases := map[string]string{
		`\u@\h:\w\$ `:                    "ann@box:/tmp$ ",
		`\H \W`:                          "box.example tmp",
		`\[\e[1;32m\]\u\[\e[0m\] `:       "\x01\x1b[1;32m\x02ann\x01\x1b[0m\x02 ",
		`\[\033[0m\]$WHERE ${WHERE}\n> `: "\x01\x1b[0m\x02here here\n> ",
		`a\\b \q`:                        `a\b \q`,
	}
	for ps1, want := range cases {
		if _, err := sh.Run(context.Background(), "PS1='"+ps1+"'"); err != nil {
			t.Fatal(err)
		}
		got, ok := sh.PS1()
		if !ok || got != want {
			t.Errorf("PS1=%q: got %q %v, want %q", ps1, got, ok, want)
		}
	}
	if _, err := sh.Run(context.Background(), "cd; PS1='\\w'"); err != nil {
		t.Fatal(err)
	}
	if got, _ := sh.PS1(); got != "~" {
		t.Errorf("home as \\w: %q", got)
	}
	if got := promptCells("\x01\x1b[1;32m\x02ann\x01xx\x02 "); string(got) != "ann " {
		t.Errorf("cells %q", string(got))
	}
}
