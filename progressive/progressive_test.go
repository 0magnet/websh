package progressive

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

// term is a terminal's far end: it records what the program wrote and
// answers from a script.
type term struct {
	wrote  bytes.Buffer
	answer io.Reader
}

func (t *term) Write(p []byte) (int, error) { return t.wrote.Write(p) }
func (t *term) Read(p []byte) (int, error)  { return t.answer.Read(p) }

// slow hands its data over a byte at a time, as a slow link would.
type slow struct{ r io.Reader }

func (s slow) Read(p []byte) (int, error) { return s.r.Read(p[:1]) }

func TestProbe(t *testing.T) {
	host := &Caps{V: 1, Host: "websh", Trust: "local", DPR: 1, Cols: 80, Rows: 24, Features: []string{"place", "font"}}
	host.Cell.W, host.Cell.H = 9, 19.7
	for name, tc := range map[string]struct {
		answer  string
		caps    bool
		rest    string
		wantErr error
	}{
		"websh":           {Reply(host) + "\x1b[?1;2c", true, "", nil},
		"websh, BEL":      {strings.TrimSuffix(Reply(host), "\x1b\\") + "\x07" + "\x1b[?62;22c", true, "", nil},
		"any terminal":    {"\x1b[?64;1;2;6;22c", false, "", nil},
		"keys meanwhile":  {"a" + Reply(host) + "b\x1b[?1;2cc", true, "abc", nil},
		"other report":    {"\x1b[?2026;2$y\x1b[?6c", false, "\x1b[?2026;2$y", nil},
		"answers nothing": {"", false, "", ErrNoAnswer},
	} {
		t.Run(name, func(t *testing.T) {
			Set(nil)
			var r io.Reader = slow{strings.NewReader(tc.answer)}
			if tc.wantErr != nil {
				r = blocking{}
			}
			tt := &term{answer: r}
			caps, in, err := Probe(tt, tt, 200*time.Millisecond)
			if err != tc.wantErr {
				t.Fatalf("err %v, want %v", err, tc.wantErr)
			}
			var rest []byte
			if tc.wantErr == nil {
				var rerr error
				if rest, rerr = io.ReadAll(in); rerr != nil { // the program's input from here: nothing lost, nothing extra
					t.Fatal(rerr)
				}
			}
			if tt.wrote.String() != Query+kittyQuery+xtversion+"\x1b[c" {
				t.Errorf("wrote %q", tt.wrote.String())
			}
			if (caps != nil) != tc.caps {
				t.Fatalf("caps %+v", caps)
			}
			if string(rest) != tc.rest {
				t.Errorf("rest %q, want %q", rest, tc.rest)
			}
			if tc.caps && (!caps.Has("place") || caps.Has("ship") || caps.Cell.H != 19.7 || Current() != caps) {
				t.Errorf("caps %+v", caps)
			}
		})
	}
	var none *Caps
	if none.Has("place") {
		t.Error("nil caps offer something")
	}
}

// blocking is a terminal that never answers.
type blocking struct{}

func (blocking) Read([]byte) (int, error) { select {} }

// TestProbeATerminal: a terminal that is not websh, asked the same way, is
// found for what it can do itself: kitty graphics, sixel, its name.
func TestProbeATerminal(t *testing.T) {
	for name, tc := range map[string]struct {
		answer string
		host   string
		feats  []string
	}{
		"kitty":       {"\x1b_Gi=31;OK\x1b\\\x1bP>|kitty(0.35.2)\x1b\\\x1b[?62;c", "kitty(0.35.2)", []string{"kitty-graphics"}},
		"sixel":       {"\x1bP>|XTerm(390)\x1b\\\x1b[?63;1;2;4;6;9;15;22c", "XTerm(390)", []string{"sixel"}},
		"no graphics": {"\x1b_Gi=31;ENOTSUPPORTED:x\x1b\\\x1b[?1;2c", "", nil},
		"just a name": {"\x1bP>|foot(1.16)\x1b\\\x1b[?62;22c", "foot(1.16)", nil},
	} {
		t.Run(name, func(t *testing.T) {
			Set(nil)
			tt := &term{answer: slow{strings.NewReader(tc.answer)}}
			caps, in, err := Probe(tt, tt, 200*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			rest, err := io.ReadAll(in)
			if err != nil {
				t.Fatal(err)
			}
			if len(rest) != 0 {
				t.Errorf("left %q", rest)
			}
			if tc.host == "" && len(tc.feats) == 0 {
				if caps != nil {
					t.Errorf("caps %+v for a plain terminal", caps)
				}
				return
			}
			if caps == nil || caps.Host != tc.host || strings.Join(caps.Features, ",") != strings.Join(tc.feats, ",") {
				t.Errorf("caps %+v", caps)
			}
		})
	}
}
