// progcheck reports what the terminal it runs in can do: whether it is a
// progressive terminal host and what it offers (websh's PROTOCOL.md), and
// the standard and kitty extensions a program may use — pictures, keyboard,
// sizes, colors, modes. It asks every question at once, then DA1, which
// every terminal answers last, so it waits for nothing it need not.
//
//	progcheck
//
// Run it in websh (it is on the PATH as a program from the filesystem), or
// in any terminal on a desktop.
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/0magnet/websh/progressive"
)

// queries are asked in this order; DA1 goes last.
var queries = []struct{ name, seq string }{
	{"progressive terminal (OSC 7337 caps)", progressive.Query},
	{"device attributes 2", "\x1b[>c"},
	{"terminal name (XTVERSION)", "\x1b[>q"},
	{"kitty keyboard protocol", "\x1b[?u"},
	{"kitty graphics protocol", "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"},
	{"cell size in pixels", "\x1b[16t"},
	{"text area in pixels", "\x1b[14t"},
	{"size in cells", "\x1b[18t"},
	{"foreground color", "\x1b]10;?\x1b\\"},
	{"background color", "\x1b]11;?\x1b\\"},
	{"pointer shapes (OSC 22)", "\x1b]22;?pointer,text\x1b\\"},
	{"synchronized output (2026)", "\x1b[?2026$p"},
	{"bracketed paste (2004)", "\x1b[?2004$p"},
	{"focus events (1004)", "\x1b[?1004$p"},
	{"SGR mouse (1006)", "\x1b[?1006$p"},
	{"grapheme clusters (2027)", "\x1b[?2027$p"},
}

const da1 = "\x1b[c"

// result is one line of the report.
type result struct{ name, value string }

// run asks and reads the answers.
func run(r io.Reader, w io.Writer, timeout time.Duration) ([]result, error) {
	var q strings.Builder
	for _, x := range queries {
		q.WriteString(x.seq)
	}
	q.WriteString(da1)
	if _, err := io.WriteString(w, q.String()); err != nil {
		return nil, err
	}
	got := make(chan []byte)
	go func() {
		var all []byte
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			all = append(all, buf[:n]...)
			if da1Reply.Match(all) || err != nil {
				got <- all
				return
			}
		}
	}()
	select {
	case all := <-got:
		return parse(all), nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("the terminal did not answer DA1 in %v", timeout)
	}
}

var (
	da1Reply = regexp.MustCompile(`\x1b\[\?([0-9;]*)c`)
	capsRe   = regexp.MustCompile(`\x1b\]7337;caps;([A-Za-z0-9+/=]*)(?:\x1b\\|\x07)`)
	da2Re    = regexp.MustCompile(`\x1b\[>([0-9;]*)c`)
	xtRe     = regexp.MustCompile(`\x1bP>\|([^\x1b]*)\x1b\\`)
	kkbRe    = regexp.MustCompile(`\x1b\[\?([0-9]+)u`)
	kgfxRe   = regexp.MustCompile(`\x1b_Gi=31;([^\x1b]*)\x1b\\`)
	cellRe   = regexp.MustCompile(`\x1b\[6;([0-9]+);([0-9]+)t`)
	areaRe   = regexp.MustCompile(`\x1b\[4;([0-9]+);([0-9]+)t`)
	sizeRe   = regexp.MustCompile(`\x1b\[8;([0-9]+);([0-9]+)t`)
	fgRe     = regexp.MustCompile(`\x1b\]10;([^\x1b\x07]*)(?:\x1b\\|\x07)`)
	bgRe     = regexp.MustCompile(`\x1b\]11;([^\x1b\x07]*)(?:\x1b\\|\x07)`)
	ptrRe    = regexp.MustCompile(`\x1b\]22;([^\x1b\x07]*)(?:\x1b\\|\x07)`)
	modeRe   = regexp.MustCompile(`\x1b\[\?([0-9]+);([0-9])\$y`)
)

// parse turns the answers into the report.
func parse(all []byte) []result {
	none := "no answer"
	sub := func(re *regexp.Regexp) []string {
		if m := re.FindSubmatch(all); m != nil {
			out := make([]string, len(m)-1)
			for i := range out {
				out[i] = string(m[i+1])
			}
			return out
		}
		return nil
	}
	var res []result
	add := func(name, value string) { res = append(res, result{name, value}) }

	if m := sub(capsRe); m != nil {
		var c progressive.Caps
		if b, err := base64.StdEncoding.DecodeString(m[0]); err == nil && json.Unmarshal(b, &c) == nil {
			add(queries[0].name, fmt.Sprintf("yes: %s %s, trust %s, offers %s", c.Host, c.Version, c.Trust, strings.Join(c.Features, " ")))
		} else {
			add(queries[0].name, "an answer it could not read")
		}
	} else {
		add(queries[0].name, "no — cells only")
	}
	if m := sub(da2Re); m != nil {
		add(queries[1].name, m[0])
	} else {
		add(queries[1].name, none)
	}
	if m := sub(xtRe); m != nil {
		add(queries[2].name, m[0])
	} else {
		add(queries[2].name, none)
	}
	if m := sub(kkbRe); m != nil {
		add(queries[3].name, "yes (flags now "+m[0]+")")
	} else {
		add(queries[3].name, "no")
	}
	if m := sub(kgfxRe); m != nil {
		v := "no: " + m[0]
		if m[0] == "OK" {
			v = "yes"
		}
		add(queries[4].name, v)
	} else {
		add(queries[4].name, "no")
	}
	pair := func(i int, re *regexp.Regexp, unit string) {
		if m := sub(re); m != nil {
			add(queries[i].name, m[1]+"×"+m[0]+" "+unit)
		} else {
			add(queries[i].name, none)
		}
	}
	pair(5, cellRe, "px")
	pair(6, areaRe, "px")
	pair(7, sizeRe, "cells")
	for i, re := range []*regexp.Regexp{fgRe, bgRe} {
		if m := sub(re); m != nil {
			add(queries[8+i].name, m[0])
		} else {
			add(queries[8+i].name, none)
		}
	}
	if m := sub(ptrRe); m != nil {
		add(queries[10].name, "yes ("+m[0]+")")
	} else {
		add(queries[10].name, "no")
	}
	modes := map[string]string{}
	for _, m := range modeRe.FindAllSubmatch(all, -1) {
		modes[string(m[1])] = string(m[2])
	}
	for i, mode := range []string{"2026", "2004", "1004", "1006", "2027"} {
		v, ok := modes[mode]
		switch {
		case !ok:
			add(queries[11+i].name, none)
		case v == "0":
			add(queries[11+i].name, "not recognized")
		case v == "1" || v == "3":
			add(queries[11+i].name, "supported, on")
		default:
			add(queries[11+i].name, "supported, off")
		}
	}
	if m := sub(da1Reply); m != nil {
		attrs := strings.Split(m[0], ";")
		sixel := "no"
		for _, a := range attrs[1:] {
			if a == "4" {
				sixel = "yes"
			}
		}
		add("sixel graphics (DA1 attribute 4)", sixel)
		add("device attributes 1", m[0])
	}
	return res
}

// report writes the results as a table.
func report(w io.Writer, res []result) {
	width := 0
	for _, r := range res {
		width = max(width, len(r.name))
	}
	var b bytes.Buffer
	for _, r := range res {
		fmt.Fprintf(&b, "%-*s  %s\r\n", width, r.name, r.value)
	}
	w.Write(b.Bytes()) //nolint:errcheck,gosec // the terminal; nowhere else to report to
}
