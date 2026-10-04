//go:build !js

package mosh

import (
	"bytes"
	"context"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/0magnet/wisp"
)

// wispServer is an in-process Wisp server that dials the host's network.
func wispServer(t *testing.T) string {
	t.Helper()
	srv, err := wisp.NewServer(wisp.Config{Egress: &wisp.DirectEgress{}})
	if err != nil {
		t.Fatal(err)
	}
	hs := httptest.NewServer(srv)
	t.Cleanup(hs.Close)
	return "ws" + strings.TrimPrefix(hs.URL, "http")
}

// udpEcho answers every datagram with the same bytes, prefixed.
func udpEcho(t *testing.T) uint16 {
	t.Helper()
	pc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() }) //nolint:errcheck,gosec // test teardown
	go func() {
		buf := make([]byte, 65535)
		for {
			n, from, err := pc.ReadFromUDP(buf)
			if err != nil {
				return
			}
			pc.WriteToUDP(append([]byte("echo:"), buf[:n]...), from) //nolint:errcheck,gosec // best effort, like UDP
		}
	}()
	return uint16(pc.LocalAddr().(*net.UDPAddr).Port) //nolint:gosec // a port fits
}

func TestConnOverWispUDP(t *testing.T) {
	url := wispServer(t)
	port := udpEcho(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wc, conn, err := Dial(ctx, url, "127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	defer wc.Close()   //nolint:errcheck // test teardown
	defer conn.Close() //nolint:errcheck // test teardown

	// Datagram boundaries survive: three writes, three reads.
	msgs := [][]byte{[]byte("one"), bytes.Repeat([]byte{0xab}, 1400), {0}}
	for _, m := range msgs {
		if n, err := conn.Write(m); err != nil || n != len(m) {
			t.Fatalf("Write: %d, %v", n, err)
		}
	}
	buf := make([]byte, 2048)
	for _, m := range msgs {
		if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatal(err)
		}
		n, err := conn.Read(buf)
		if err != nil {
			t.Fatal(err)
		}
		if want := append([]byte("echo:"), m...); !bytes.Equal(buf[:n], want) {
			t.Fatalf("got %d bytes %q, want %d", n, buf[:min(n, 20)], len(want))
		}
	}

	// A deadline with nothing to read times out the way mosh-go expects.
	if err := conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(buf); !os.IsTimeout(err) {
		t.Fatalf("idle Read = %v, want a timeout", err)
	}

	// After Close, reads and writes fail instead of hanging.
	if err := conn.Close(); err != nil {
		t.Logf("Close: %v", err)
	}
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(buf); err == nil || os.IsTimeout(err) {
		t.Fatalf("Read after Close = %v", err)
	}
	if _, err := conn.Write([]byte("x")); err == nil {
		t.Fatal("Write after Close succeeded")
	}
}
