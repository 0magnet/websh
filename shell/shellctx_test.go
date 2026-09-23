package shell

import (
	"context"
	"testing"
)

func TestFromContext(t *testing.T) {
	if got := FromContext(context.Background()); got != nil {
		t.Errorf("FromContext of a bare context = %v, want nil", got)
	}
	sh := &Shell{}
	if got := FromContext(WithShell(context.Background(), sh)); got != sh {
		t.Errorf("FromContext = %v, want the shell put in", got)
	}
}
