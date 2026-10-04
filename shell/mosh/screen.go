//go:build !tinygo

package mosh

import (
	"sync"

	moshgo "github.com/unixshells/mosh-go"
	vt "github.com/unixshells/vt-go"
)

// screen turns mosh's state diffs into terminal output.
//
// A mosh-server does not send a stream of output. It sends diffs between
// numbered screen states — "from state 7 to state 9, do this" — and resends
// them against whatever base the client last acknowledged. Writing each diff
// straight to the terminal therefore draws garbage as soon as one is resent
// or arrives out of order. So every diff is applied to a copy of the state it
// names as its base, the result is kept under its own number, and the
// terminal is moved from what it shows to the newest state by a diff of two
// framebuffers.
//
// This follows the state tracker in mosh-go's own browser client
// (cmd/mosh-wasm/state.go, MIT), which is in its package main and so cannot
// be imported.
type screen struct {
	mu sync.Mutex

	cols, rows int
	shadow     *vt.Emulator // the emulator diffs are applied in
	shadowNum  uint64       // the state shadow currently holds

	states    map[uint64]*moshgo.Framebuffer
	latest    uint64
	displayed *moshgo.Framebuffer

	out []byte // terminal output not yet written
}

func newScreen(cols, rows int) *screen {
	return &screen{
		cols:   cols,
		rows:   rows,
		shadow: vt.NewEmulator(cols, rows),
		states: map[uint64]*moshgo.Framebuffer{},
	}
}

// apply takes one diff from state oldNum to state newNum. States below
// throwaway are ones the server will never diff against again.
func (s *screen) apply(diff []byte, oldNum, newNum, throwaway uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, seen := s.states[newNum]; seen {
		return
	}
	if oldNum == s.shadowNum {
		if _, ok := s.states[oldNum]; !ok {
			s.states[oldNum] = moshgo.SnapshotEmulator(s.shadow, true)
		}
	}
	base, ok := s.states[oldNum]
	switch {
	case !ok:
		// No such base — after a resize, say. Start from a blank screen.
		s.shadow = vt.NewEmulator(s.cols, s.rows)
		s.states[oldNum] = moshgo.SnapshotEmulator(s.shadow, true)
	case s.shadowNum != oldNum:
		s.shadow = vt.NewEmulator(s.cols, s.rows)
		s.shadow.Write(base.Diff(nil)) //nolint:errcheck,gosec // an in-memory emulator
	}

	instrs, err := moshgo.UnmarshalHostMessage(diff)
	if err != nil {
		return
	}
	for _, hi := range instrs {
		if len(hi.Hoststring) > 0 {
			s.shadow.Write(hi.Hoststring) //nolint:errcheck,gosec // an in-memory emulator
		}
	}
	s.shadowNum = newNum
	s.states[newNum] = moshgo.SnapshotEmulator(s.shadow, true)
	if newNum > s.latest {
		s.latest = newNum
	}
	for n := range s.states {
		if n < throwaway && n != s.latest {
			delete(s.states, n)
		}
	}

	if latest := s.states[s.latest]; latest != nil {
		s.out = append(s.out, latest.Diff(s.displayed)...)
		s.displayed = latest
	}
}

// take returns the output accumulated since the last call.
func (s *screen) take() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.out
	s.out = nil
	return out
}

// resize starts over at a new size; the server redraws after a resize.
func (s *screen) resize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
	s.shadow = vt.NewEmulator(cols, rows)
	s.shadowNum = 0
	s.states = map[uint64]*moshgo.Framebuffer{}
	s.displayed = nil
}
