package sed

import (
	"errors"
	"strings"
	"testing"
)

func TestRunAcrossReaders(t *testing.T) {
	p, err := Compile("$!d", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	// The first reader's last line has no newline; it still ends a line.
	if err := p.Run(&out, strings.NewReader("a\nb"), strings.NewReader("c\nd\n")); err != nil {
		t.Fatal(err)
	}
	if out.String() != "d\n" {
		t.Fatalf("got %q", out.String())
	}
}

func TestQuitCode(t *testing.T) {
	p, err := Compile("2q5", Options{})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := p.Run(&out, strings.NewReader("a\nb\nc\n")); !errors.Is(err, ErrQuit) {
		t.Fatalf("err = %v", err)
	}
	if out.String() != "a\nb\n" || p.ExitCode() != 5 {
		t.Fatalf("got %q code %d", out.String(), p.ExitCode())
	}
}

func TestTranslate(t *testing.T) {
	for _, c := range []struct {
		in, want string
		ere      bool
	}{
		{`\(a\)\{2\}`, `(a){2}`, false},
		{`(a){2}+?|`, `\(a\)\{2\}\+\?\|`, false},
		{`a\+b\?c\|d`, `a+b?c|d`, false},
		{`*a`, `\*a`, false},
		{`^*a`, `^\*a`, false},
		{`a^b$c$`, `a\^b\$c$`, false},
		{`[]a\]`, `[\]a\\]`, false},
		{`[[:digit:]x[]`, `[[:digit:]x\[]`, false},
		{`\<w\>`, `\bw\b`, false},
		{`(a|b)+`, `(a|b)+`, true},
		{`\(`, `\(`, true},
	} {
		got, err := translate(c.in, c.ere, '/')
		if err != nil || got != c.want {
			t.Errorf("translate(%q, ere=%v) = %q, %v; want %q", c.in, c.ere, got, err, c.want)
		}
	}
}
