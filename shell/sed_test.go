package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/0magnet/afero"
)

// The expected outputs were taken from GNU sed 4.10 on the same input.
const sedInput = "one\ntwo\nthree\nfour\nfive\n"

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// runSedCmd runs sed with args over in.txt holding sedInput, in a fresh shell,
// and returns stdout+stderr and the filesystem afterwards.
func runSedCmd(t *testing.T, args ...string) (string, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	if err := Seed(fs); err != nil {
		t.Fatal(err)
	}
	if err := afero.WriteFile(fs, "/home/user/in.txt", []byte(sedInput), 0o644); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	sh, err := New(fs, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = shellQuote(a)
	}
	line := "sed " + strings.Join(q, " ")
	if _, err := sh.Run(context.Background(), line); err != nil && !strings.Contains(err.Error(), "exit status") {
		t.Fatalf("%s: %v", line, err)
	}
	return out.String(), fs
}

func TestSedScripts(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"-n", "2p"}, "two\n"},
		{[]string{"2d"}, "one\nthree\nfour\nfive\n"},
		{[]string{"-n", "$p"}, "five\n"},
		{[]string{"-n", "/t/p"}, "two\nthree\n"},
		{[]string{"2,4d"}, "one\nfive\n"},
		{[]string{"-n", "2,4!p"}, "one\nfive\n"},
		{[]string{"-n", "/two/,/four/p"}, "two\nthree\nfour\n"},
		{[]string{"/two/,+1d"}, "one\nfour\nfive\n"},
		{[]string{"-n", "0~2p"}, "two\nfour\n"},
		{[]string{"-n", "1~2p"}, "one\nthree\nfive\n"},
		{[]string{"0,/o/d"}, "two\nthree\nfour\nfive\n"},
		{[]string{"1,/o/d"}, "three\nfour\nfive\n"},
		{[]string{"/one/,/t/d"}, "three\nfour\nfive\n"},
		{[]string{"2,1p", "-n"}, "two\n"},
		{[]string{"s/o/0/"}, "0ne\ntw0\nthree\nf0ur\nfive\n"},
		{[]string{"s/e/E/2"}, "one\ntwo\nthreE\nfour\nfive\n"},
		{[]string{"s/e/E/2g"}, "one\ntwo\nthreE\nfour\nfive\n"},
		{[]string{"s/O/X/I"}, "Xne\ntwX\nthree\nfXur\nfive\n"},
		{[]string{`s/\(t\)\(w\)/\2\1/`}, "one\nwto\nthree\nfour\nfive\n"},
		{[]string{"-E", `s/(t)(w)/\2\1/`}, "one\nwto\nthree\nfour\nfive\n"},
		{[]string{"s/[aeiou]/<&>/g"}, "<o>n<e>\ntw<o>\nthr<e><e>\nf<o><u>r\nf<i>v<e>\n"},
		{[]string{"s|o|/|g"}, "/ne\ntw/\nthree\nf/ur\nfive\n"},
		{[]string{`s/o\+/X/`}, "Xne\ntwX\nthree\nfXur\nfive\n"},
		{[]string{"s/o+/X/"}, sedInput},
		{[]string{"-E", "s/o+/X/"}, "Xne\ntwX\nthree\nfXur\nfive\n"},
		{[]string{`s/^\(.\)\(.*\)$/\U\1\E\2/`}, "One\nTwo\nThree\nFour\nFive\n"},
		{[]string{`s/.*/\u&/`}, "One\nTwo\nThree\nFour\nFive\n"},
		{[]string{"y/otf/OTF/"}, "One\nTwO\nThree\nFOur\nFive\n"},
		{[]string{"2q"}, "one\ntwo\n"},
		{[]string{"2Q"}, "one\n"},
		{[]string{"-n", "$="}, "5\n"},
		{[]string{"2a\\\nafter"}, "one\ntwo\nafter\nthree\nfour\nfive\n"},
		{[]string{"2a after"}, "one\ntwo\nafter\nthree\nfour\nfive\n"},
		{[]string{"2i before"}, "one\nbefore\ntwo\nthree\nfour\nfive\n"},
		{[]string{"2c changed"}, "one\nchanged\nthree\nfour\nfive\n"},
		{[]string{"2,3c changed"}, "one\nchanged\nfour\nfive\n"},
		{[]string{"2,3!c X"}, "X\ntwo\nthree\nX\nX\n"},
		{[]string{"-n", "/two/{n;p}"}, "three\n"},
		{[]string{"-n", "2{p;p}"}, "two\ntwo\n"},
		{[]string{"-n", "/t/{/w/p}"}, "two\n"},
		{[]string{`$!N;s/\n/-/`}, "one-two\nthree-four\nfive\n"},
		{[]string{`N;N;s/\n/+/g`}, "one+two+three\nfour\nfive\n"},
		{[]string{"-n", "h;n;G;p"}, "two\none\nfour\nthree\n"},
		{[]string{"1!G;h;$!d"}, "five\nfour\nthree\ntwo\none\n"},
		{[]string{`:a;N;$!ba;s/\n/,/g`}, "one,two,three,four,five\n"},
		{[]string{"s/x/y/;ta;s/$/!/;:a"}, "one!\ntwo!\nthree!\nfour!\nfive!\n"},
		{[]string{"s/o/O/;Ta;s/$/+/;:a"}, "One+\ntwO+\nthree\nfOur+\nfive\n"},
		{[]string{"-e", "1d", "-e", "3d"}, "two\nfour\nfive\n"},
		{[]string{"1d;3d"}, "two\nfour\nfive\n"},
		{[]string{`s/\(o\)\{2\}/X/`}, sedInput},
		{[]string{`s/e\{1,\}/E/`}, "onE\ntwo\nthrE\nfour\nfivE\n"},
		{[]string{"-E", `s/e{1,}/E/`}, "onE\ntwo\nthrE\nfour\nfivE\n"},
		{[]string{"s/a|b/X/"}, sedInput},
		{[]string{`s/o\|e/X/g`}, "XnX\ntwX\nthrXX\nfXur\nfivX\n"},
		{[]string{"-E", "s/o|e/X/g"}, "XnX\ntwX\nthrXX\nfXur\nfivX\n"},
		{[]string{"s/(/X/"}, sedInput},
		{[]string{"s/*/X/"}, sedInput},
		{[]string{"s/^*/X/"}, sedInput},
		{[]string{"s/t*/X/g"}, "XoXnXeX\nXwXoX\nXhXrXeXeX\nXfXoXuXrX\nXfXiXvXeX\n"},
		{[]string{"z;s/^$/empty/"}, "empty\nempty\nempty\nempty\nempty\n"},
		{[]string{"-n", "/[[:upper:]]/p;/[[:digit:]]/p;/f[[:alpha:]]/p"}, "four\nfive\n"},
		{[]string{"s/[]]/X/"}, sedInput},
		{[]string{"$d"}, "one\ntwo\nthree\nfour\n"},
		{[]string{"2!d"}, "two\n"},
		{[]string{`s/e/\n/;P;D`}, "on\n\ntwo\nthr\n\n\nfour\nfiv\n\n"},
		{[]string{"s/o/[&]/3"}, sedInput},
		{[]string{"x;$!d;x"}, "five\n"},
		{[]string{"="}, "1\none\n2\ntwo\n3\nthree\n4\nfour\n5\nfive\n"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, " "), func(t *testing.T) {
			args := append(append([]string{}, c.args...), "in.txt")
			if got, _ := runSedCmd(t, args...); got != c.want {
				t.Errorf("sed %q\n got %q\nwant %q", c.args, got, c.want)
			}
		})
	}
}

func TestSedStdinAndMissingNewline(t *testing.T) {
	if got := run(t, "seq 5 | sed -n 2p"); got != "2\n" {
		t.Errorf("seq 5 | sed -n 2p = %q", got)
	}
	if got := run(t, "printf 'a b\\n' | sed 's/\\(a\\) \\(b\\)/\\2 \\1/'"); got != "b a\n" {
		t.Errorf("swap = %q", got)
	}
	// A last line without a newline stays that way, unless more follows.
	if got := run(t, "printf a | sed p"); got != "a\na" {
		t.Errorf("printf a | sed p = %q", got)
	}
	if got := run(t, "printf x | sed 'a end'"); got != "x\nend\n" {
		t.Errorf("printf x | sed 'a end' = %q", got)
	}
}

func TestSedSeparateFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	if err := Seed(fs); err != nil {
		t.Fatal(err)
	}
	afero.WriteFile(fs, "/home/user/a", []byte("1\n2\n3\n"), 0o644) //nolint:errcheck,gosec
	afero.WriteFile(fs, "/home/user/b", []byte("x\ny\n"), 0o644)    //nolint:errcheck,gosec
	var out strings.Builder
	sh, err := New(fs, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"sed -n '$p;1p' a b", "sed -s -n '$p;1p' a b"} {
		if _, err := sh.Run(context.Background(), line); err != nil {
			t.Fatal(err)
		}
	}
	if want := "1\ny\n" + "1\n3\nx\ny\n"; out.String() != want {
		t.Errorf("got %q want %q", out.String(), want)
	}
}

func TestSedInPlace(t *testing.T) {
	out, fs := runSedCmd(t, "-i", "s/o/0/g;3d", "in.txt")
	if out != "" {
		t.Errorf("-i wrote to stdout: %q", out)
	}
	got := mustRead(t, fs, "/home/user/in.txt")
	if want := "0ne\ntw0\nf0ur\nfive\n"; string(got) != want {
		t.Errorf("in place: %q want %q", got, want)
	}

	_, fs = runSedCmd(t, "-i.bak", "-n", "1p", "in.txt")
	got = mustRead(t, fs, "/home/user/in.txt")
	bak := mustRead(t, fs, "/home/user/in.txt.bak")
	if string(got) != "one\n" || string(bak) != sedInput {
		t.Errorf("-i.bak: file %q backup %q", got, bak)
	}
}

func TestSedWriteAndRead(t *testing.T) {
	out, fs := runSedCmd(t, "-n", "/t/w hits\n$r hits", "in.txt")
	if out != "two\nthree\n" {
		t.Errorf("r output %q", out)
	}
	if got := mustRead(t, fs, "/home/user/hits"); string(got) != "two\nthree\n" {
		t.Errorf("w file = %q", got)
	}
}

func TestSedErrors(t *testing.T) {
	for _, args := range [][]string{
		{"s/a/b"},
		{"k"},
		{"{p"},
		{"b nowhere"},
		{`s/\(a\)\1/x/`},
	} {
		out, _ := runSedCmd(t, append(args, "in.txt")...)
		if !strings.HasPrefix(out, "sed: ") {
			t.Errorf("sed %q: want an error, got %q", args, out)
		}
	}
}

func mustRead(t *testing.T, fs afero.Fs, path string) []byte {
	t.Helper()
	b, err := afero.ReadFile(fs, path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
