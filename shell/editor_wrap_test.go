package shell

import (
	"strings"
	"testing"

	"github.com/0magnet/xterm-go/vt"
)

// wrapRig is a line editor drawing on a headless xterm-go terminal, which is
// the oracle for where its output lands.
type wrapRig struct {
	t      *testing.T
	term   *vt.Terminal
	ed     *LineEditor
	prompt string
}

func newWrapRig(t *testing.T, cols int, prompt string) *wrapRig {
	t.Helper()
	opts := vt.NewOptions()
	opts.Cols, opts.Rows = cols, 12
	r := &wrapRig{t: t, term: vt.NewTerminal(opts), prompt: prompt}
	r.ed = &LineEditor{
		Echo:   r.term.WriteString,
		Prompt: func() string { return r.prompt },
		Cols:   r.term.Cols,
	}
	r.term.WriteString(PromptText(prompt))
	return r
}

// screen is the terminal's rows down to the last one with text, trimmed.
func (r *wrapRig) screen() []string {
	b := r.term.Buffer()
	var rows []string
	for y := 0; y < r.term.Rows(); y++ {
		rows = append(rows, strings.TrimRight(b.Lines.Get(b.YBase+y).TranslateToString(true, 0, -1), " "))
	}
	for len(rows) > 0 && rows[len(rows)-1] == "" {
		rows = rows[:len(rows)-1]
	}
	return rows
}

// expect checks the screen and the cursor's row and column.
func (r *wrapRig) expect(rows []string, y, x int) {
	r.t.Helper()
	got := r.screen()
	if strings.Join(got, "|") != strings.Join(rows, "|") {
		r.t.Fatalf("screen\n got %q\nwant %q", got, rows)
	}
	b := r.term.Buffer()
	if b.Y != y || b.X != x {
		r.t.Fatalf("cursor at row %d col %d, want row %d col %d (screen %q)", b.Y, b.X, y, x, got)
	}
}

func TestEditorBackspaceAcrossWrap(t *testing.T) {
	r := newWrapRig(t, 10, "$ ")
	r.ed.Input("abcdefghijkl")
	r.expect([]string{"$ abcdefgh", "ijkl"}, 1, 4)
	r.ed.Input(strings.Repeat("\x7f", 5))
	r.expect([]string{"$ abcdefg"}, 0, 9)
	r.ed.Input("XY")
	r.expect([]string{"$ abcdefgX", "Y"}, 1, 1)
	if r.ed.Line() != "abcdefgXY" {
		t.Fatalf("line %q", r.ed.Line())
	}
}

func TestEditorExactlyFilledRow(t *testing.T) {
	r := newWrapRig(t, 10, "$ ")
	r.ed.Input("abcdefgh")
	r.expect([]string{"$ abcdefgh"}, 1, 0)
	r.ed.Input("\x7f")
	r.expect([]string{"$ abcdefg"}, 0, 9)
	r.ed.Input("h\x1b[D\x1b[C")
	r.expect([]string{"$ abcdefgh"}, 1, 0)
}

func TestEditorMidLineEditAcrossWrap(t *testing.T) {
	r := newWrapRig(t, 10, "$ ")
	r.ed.Input("0123456789abcdefghij")
	r.expect([]string{"$ 01234567", "89abcdefgh", "ij"}, 2, 2)
	// Home, then right 6: before '6', on the first row.
	r.ed.Input("\x01" + strings.Repeat("\x1b[C", 6))
	r.expect([]string{"$ 01234567", "89abcdefgh", "ij"}, 0, 8)
	r.ed.Input("XYZ")
	r.expect([]string{"$ 012345XY", "Z6789abcde", "fghij"}, 1, 1)
	// Left across the wrap, then delete under the cursor.
	r.ed.Input("\x1b[D\x1b[D\x1b[3~")
	r.expect([]string{"$ 012345XZ", "6789abcdef", "ghij"}, 0, 9)
	// Backspace from the start of a row joins it to the one above.
	r.ed.Input("\x1b[C\x7f")
	r.expect([]string{"$ 012345X6", "789abcdefg", "hij"}, 0, 9)
	if r.ed.Line() != "012345X6789abcdefghij" {
		t.Fatalf("line %q", r.ed.Line())
	}
	// Ctrl+K from the middle of the second row clears what follows.
	r.ed.Input("\x1b[C\x1b[C\x0b")
	r.expect([]string{"$ 012345X6", "7"}, 1, 1)
}

func TestEditorHistoryLongerThanWidth(t *testing.T) {
	r := newWrapRig(t, 10, "$ ")
	long := "echo the quick brown fox jumps"
	r.ed.AddHistory(long)
	r.ed.Input("ls")
	r.ed.Input("\x1b[A")
	r.expect([]string{"$ echo the", " quick bro", "wn fox jum", "ps"}, 3, 2)
	r.ed.Input("\x1b[B")
	r.expect([]string{"$ ls"}, 0, 4)
	r.ed.Input("\x1b[A\x01")
	r.expect([]string{"$ echo the", " quick bro", "wn fox jum", "ps"}, 0, 2)
	r.ed.Input("\x1b[B")
	r.expect([]string{"$ ls"}, 0, 4)
	if r.ed.Line() != "ls" {
		t.Fatalf("line %q", r.ed.Line())
	}
}

func TestEditorWrappedPrompt(t *testing.T) {
	// 13 cells of host, with colors and a marked title that take none.
	prompt := "\x01\x1b]0;title\a\x02\x1b[1;32mabcdefghijklm\x1b[0m$ "
	r := newWrapRig(t, 10, prompt)
	r.expect([]string{"abcdefghij", "klm$"}, 1, 5)
	r.ed.Input("123456")
	r.expect([]string{"abcdefghij", "klm$ 12345", "6"}, 2, 1)
	r.ed.Input("\x7f\x7f")
	r.expect([]string{"abcdefghij", "klm$ 1234"}, 1, 9)
	r.ed.Input("\x01X")
	r.expect([]string{"abcdefghij", "klm$ X1234"}, 1, 6)
	r.ed.Input("\x15")
	r.expect([]string{"abcdefghij", "klm$ 1234"}, 1, 5)
}

func TestEditorPromptFillsRow(t *testing.T) {
	r := newWrapRig(t, 10, "abcdefgh$ ")
	r.ed.Input("x")
	r.expect([]string{"abcdefgh$", "x"}, 1, 1)
	r.ed.Input("\x7f")
	r.expect([]string{"abcdefgh$"}, 1, 0)
}

func TestEditorWideRunes(t *testing.T) {
	r := newWrapRig(t, 10, "$ ")
	r.ed.Input("abcdefg日本")
	// 日 does not fit in the last column and wraps whole.
	r.expect([]string{"$ abcdefg", "日本"}, 1, 4)
	r.ed.Input("\x7f\x7f")
	r.expect([]string{"$ abcdefg"}, 0, 9)
}

func TestEditorResizeReflows(t *testing.T) {
	r := newWrapRig(t, 20, "$ ")
	r.ed.Input("0123456789abcdefghijklmn")
	r.expect([]string{"$ 0123456789abcdefgh", "ijklmn"}, 1, 6)
	r.term.Resize(10, 12)
	r.ed.Refresh()
	r.expect([]string{"$ 01234567", "89abcdefgh", "ijklmn"}, 2, 6)
	r.ed.Input("\x7f\x7f\x7f\x7f\x7f\x7f\x7f")
	r.expect([]string{"$ 01234567", "89abcdefg"}, 1, 9)
}

func TestEditorSubmitFromMidLine(t *testing.T) {
	r := newWrapRig(t, 10, "$ ")
	var got string
	r.ed.Submit = func(l string) { got = l; r.term.WriteString(r.prompt) }
	r.ed.Input("0123456789ab\x01\r")
	if got != "0123456789ab" {
		t.Fatalf("submitted %q", got)
	}
	r.expect([]string{"$ 01234567", "89ab", "$"}, 2, 2)
}
