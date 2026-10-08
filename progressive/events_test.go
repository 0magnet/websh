package progressive

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// pieces hands its input over in the given pieces, one per Read.
type pieces struct{ p []string }

func (s *pieces) Read(b []byte) (int, error) {
	if len(s.p) == 0 {
		return 0, io.EOF
	}
	n := copy(b, s.p[0])
	s.p[0] = s.p[0][n:]
	if s.p[0] == "" {
		s.p = s.p[1:]
	}
	return n, nil
}

func TestFilter(t *testing.T) {
	click := EventSeq("globe", &Event{Type: "click", X: 0.5, Y: 0.25, Col: 40, Row: 7})
	msg := EventSeq("card", &Event{Type: "message", Data: json.RawMessage(`{"paid":"pi_1"}`)})
	split := len(click) / 2
	for name, tc := range map[string]struct {
		in     []string
		keys   string
		events []string
	}{
		"keys only":           {[]string{"abc\x1b[A"}, "abc\x1b[A", nil},
		"an event among keys": {[]string{"a" + click + "b"}, "ab", []string{"globe click"}},
		"two in one read":     {[]string{click + msg}, "", []string{"globe click", "card message"}},
		"split across reads":  {[]string{"x" + click[:split], click[split:] + "y"}, "xy", []string{"globe click"}},
		"split in the head":   {[]string{"x\x1b]73", "37;event;" + click[len(eventHead):]}, "x", []string{"globe click"}},
		"a lone escape":       {[]string{"\x1b", "[B"}, "\x1b[B", nil},
		"alt+]":               {[]string{"\x1b]", "q"}, "\x1b]q", nil},
		"another osc reply":   {[]string{"\x1b]11;rgb:0000/0000/0000\x1b\\"}, "\x1b]11;rgb:0000/0000/0000\x1b\\", nil},
		"a broken event":      {[]string{eventHead + "x;!!!\x1b\\k"}, "k", nil},
		"cut off at the end":  {[]string{"z\x1b]7337;ev"}, "z\x1b]7337;ev", nil},
	} {
		t.Run(name, func(t *testing.T) {
			r, evs := Filter(&pieces{append([]string{}, tc.in...)})
			keys, err := io.ReadAll(r)
			if err != nil {
				t.Fatal(err)
			}
			if string(keys) != tc.keys {
				t.Errorf("keys %q, want %q", keys, tc.keys)
			}
			var got []string
			for e := range evs {
				got = append(got, e.ID+" "+e.Type)
				if e.When().IsZero() {
					t.Error("no arrival time")
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.events, ",") {
				t.Errorf("events %v, want %v", got, tc.events)
			}
		})
	}
}

func TestEventFields(t *testing.T) {
	r, evs := Filter(strings.NewReader(EventSeq("card", &Event{Type: "message", Data: json.RawMessage(`{"paid":"pi_1"}`)}) +
		EventSeq("globe", &Event{Type: "dblclick", X: 0.5, Col: 40, Row: 7, Button: 0})))
	if _, err := io.ReadAll(r); err != nil {
		t.Fatal(err)
	}
	m, d := <-evs, <-evs
	if string(m.Data) != `{"paid":"pi_1"}` || d.Type != "dblclick" || d.Col != 40 || d.Row != 7 || d.X != 0.5 {
		t.Errorf("%+v %+v", m, d)
	}
}

func TestSequences(t *testing.T) {
	dec := func(s, head string) string {
		if !strings.HasPrefix(s, head) || !strings.HasSuffix(s, "\x1b\\") {
			t.Fatalf("%q", s)
		}
		b, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(strings.TrimPrefix(s, head), "\x1b\\"))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if got := dec(Place("g", Placement{Row: 2, Col: 3, W: 10, H: 5, Widget: "globe", Input: true, Events: true}), "\x1b]7337;place;g;"); got != `{"row":2,"col":3,"w":10,"h":5,"widget":"globe","input":true,"events":true}` {
		t.Errorf("place %s", got)
	}
	if got := dec(Post("g", map[string]int{"n": 1}), "\x1b]7337;post;g;"); got != `{"n":1}` {
		t.Errorf("post %s", got)
	}
	if Remove("g") != "\x1b]7337;remove;g\x1b\\" || Clear() != "\x1b]7337;clear\x1b\\" {
		t.Error("remove/clear")
	}
}

// TestShip: a widget goes in chunks that put back together into the
// document, every one but the last saying more is to come.
func TestShip(t *testing.T) {
	doc := strings.Repeat("<p>widget</p>", 1000) // 13000 bytes: five chunks
	seqs := Ship("w", []byte(doc))
	if len(seqs) != 5 {
		t.Fatalf("%d sequences", len(seqs))
	}
	var got []byte
	for i, s := range seqs {
		body := strings.TrimSuffix(strings.TrimPrefix(s, "\x1b]7337;ship;w;"), "\x1b\\")
		meta, chunk, _ := strings.Cut(body, ";")
		m, err := base64.StdEncoding.DecodeString(meta)
		if err != nil {
			t.Fatal(err)
		}
		var md struct {
			Kind string `json:"kind"`
			More bool   `json:"more"`
		}
		if err := json.Unmarshal(m, &md); err != nil || md.Kind != "html" || md.More != (i < len(seqs)-1) {
			t.Errorf("chunk %d meta %s", i, m)
		}
		if len(chunk) > 4096 {
			t.Errorf("chunk %d is %d bytes", i, len(chunk))
		}
		b, err := base64.StdEncoding.DecodeString(chunk)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, b...)
	}
	if string(got) != doc {
		t.Error("the document did not survive the trip")
	}
	if len(Ship("empty", nil)) != 1 {
		t.Error("an empty widget is still one sequence")
	}
}
