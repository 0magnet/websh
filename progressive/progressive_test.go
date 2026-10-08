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
			if tt.wrote.String() != Query+"\x1b[c" {
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
