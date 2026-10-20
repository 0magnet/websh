package pty

import "testing"

func TestWebSocketURL(t *testing.T) {
	for _, tc := range []struct{ target, base, sid, want string }{
		{"0281a1", "http://127.0.0.1:8000/pty/", "", "ws://127.0.0.1:8000/pty/0281a1"},
		{"pty?commands=update", "http://x.dmsg/", "", "ws://x.dmsg/pty?commands=update"},
		{"/pty", "https://host/a/b", "", "wss://host/pty"},
		{"wss://other/pty", "http://host/", "", "wss://other/pty"},
		{"0281a1", "http://h/vnet/8001/pty/", "ab12", "ws://h/vnet/8001/pty/0281a1?sid=ab12"},
		{"pty?sid=old", "http://h/", "new", "ws://h/pty?sid=new"},
		{"http://h/pty#frag", "http://h/", "", "ws://h/pty"},
	} {
		got, err := WebSocketURL(tc.target, tc.base, tc.sid)
		if err != nil || got != tc.want {
			t.Errorf("WebSocketURL(%q, %q, %q) = %q, %v; want %q", tc.target, tc.base, tc.sid, got, err, tc.want)
		}
	}
	for _, bad := range []string{"", "ftp://h/x"} {
		if _, err := WebSocketURL(bad, "http://h/", ""); err == nil {
			t.Errorf("WebSocketURL(%q) gave no error", bad)
		}
	}
}

func TestParseControl(t *testing.T) {
	if c, ok := ParseControl([]byte(`{"type":"session","id":"x1"}`)); !ok || c.ID != "x1" {
		t.Errorf("session message not read: %+v %v", c, ok)
	}
	if c, ok := ParseControl(ResizeMessage(100, 30)); !ok || c.Type != "resize" || c.Cols != 100 || c.Rows != 30 {
		t.Errorf("resize message not read back: %+v %v", c, ok)
	}
	for _, out := range []string{"", "plain text", "{not json", `{"id":"no type"}`} {
		if _, ok := ParseControl([]byte(out)); ok {
			t.Errorf("%q read as control", out)
		}
	}
}

func TestEscape(t *testing.T) {
	for _, tc := range []struct {
		in, out string
		quit    bool
	}{
		{"ls\r", "ls\r", false},
		{"~.", "", true},
		{"ls\r~.", "ls\r", true},
		{"a~.", "a~.", false},
		{"~~x", "~x", false},
		{"~x", "~x", false},
	} {
		var e escape
		out, quit := e.filter([]byte(tc.in))
		if string(out) != tc.out || quit != tc.quit {
			t.Errorf("filter(%q) = %q, %v; want %q, %v", tc.in, out, quit, tc.out, tc.quit)
		}
	}

	// A ~ that ends one read waits for the next.
	var e escape
	if out, quit := e.filter([]byte("~")); len(out) != 0 || quit {
		t.Fatalf("a lone ~ was sent or quit: %q %v", out, quit)
	}
	if _, quit := e.filter([]byte(".")); !quit {
		t.Fatal("~ then . across two reads did not quit")
	}
}
