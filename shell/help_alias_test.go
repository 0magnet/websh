package shell

import "testing"

// callHandler renames help so interp's unimplemented builtin does not swallow
// it. The rename used to resolve to nothing, which made help the one command
// that never ran, whether typed bare or in a pipeline.
func TestHelpSurvivesTheRename(t *testing.T) {
	renamed, err := (&Shell{}).callHandler(nil, []string{"help"}) //nolint:staticcheck // nil ctx is unused here
	if err != nil {
		t.Fatalf("callHandler: %v", err)
	}
	if len(renamed) == 0 || renamed[0] != "websh-help" {
		t.Fatalf("callHandler gave %v, want it to start with websh-help", renamed)
	}
	if _, ok := applets[renamed[0]]; !ok {
		t.Fatalf("%q does not resolve to an applet, so help cannot run", renamed[0])
	}
}

// The alias is plumbing; it should not appear in help or in /bin.
func TestAliasIsNotListed(t *testing.T) {
	for _, n := range AppletNames() {
		if n == "websh-help" {
			t.Fatal("websh-help is listed as a command; it is the rename, not a command")
		}
	}
}
