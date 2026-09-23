//go:build js && wasm

package web

import "testing"

func TestClampZoom(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{10, 10},
		{zoomMin, zoomMin},
		{zoomMax, zoomMax},
		{zoomMin - 1, zoomMin},
		{zoomMax + 100, zoomMax},
		{0, zoomMin},
	} {
		if got := clampZoom(c.in); got != c.want {
			t.Errorf("clampZoom(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
