package main

import (
	"strings"
	"testing"

	"github.com/0magnet/websh/progressive"
)

// TestParse: an answer from each kind of terminal reads as it should.
func TestParse(t *testing.T) {
	websh := progressive.Reply(&progressive.Caps{V: 1, Host: "websh", Version: "v1", Trust: "local", Features: []string{"place", "image"}})
	all := websh + "\x1b[>0;276;0c\x1bP>|websh\x1b\\\x1b[?1u\x1b_Gi=31;OK\x1b\\\x1b[6;20;9t\x1b[4;940;1872t\x1b[8;47;208t" +
		"\x1b]10;rgb:ffff/ffff/ffff\x1b\\\x1b]11;rgb:0000/0000/0000\x1b\\\x1b[?2026;2$y\x1b[?2004;1$y\x1b[?1004;0$y\x1b[?2027;2$y\x1b[?62;4;22c"
	got := map[string]string{}
	for _, r := range parse([]byte(all)) {
		got[r.name] = r.value
	}
	for name, want := range map[string]string{
		"progressive terminal (OSC 7337 caps)": "yes: websh v1, trust local, offers place image",
		"terminal name (XTVERSION)":            "websh",
		"kitty keyboard protocol":              "yes (flags now 1)",
		"kitty graphics protocol":              "yes",
		"cell size in pixels":                  "9×20 px",
		"size in cells":                        "208×47 cells",
		"background color":                     "rgb:0000/0000/0000",
		"pointer shapes (OSC 22)":              "no",
		"synchronized output (2026)":           "supported, off",
		"bracketed paste (2004)":               "supported, on",
		"focus events (1004)":                  "not recognized",
		"SGR mouse (1006)":                     "no answer",
		"sixel graphics (DA1 attribute 4)":     "yes",
	} {
		if got[name] != want {
			t.Errorf("%s: %q, want %q", name, got[name], want)
		}
	}
	plain := parse([]byte("\x1b[?1;2c"))
	if !strings.HasPrefix(plain[0].value, "no") {
		t.Errorf("a plain terminal: %+v", plain[0])
	}
}
