package shell

import (
	"context"
	"strings"
	"testing"
)

// TestReportJobs drives a shell the way the session loop does: a line, then
// the job report that precedes the next prompt.
func TestReportJobs(t *testing.T) {
	var out strings.Builder
	sh, err := New(nil, strings.NewReader(""), &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	run := func(line string) string {
		t.Helper()
		before := out.Len()
		if _, err := sh.Run(ctx, line); err != nil && !strings.Contains(err.Error(), "exit status") {
			t.Fatalf("%q: %v", line, err)
		}
		return out.String()[before:]
	}
	report := func() string {
		t.Helper()
		before := out.Len()
		sh.ReportJobs(ctx)
		return out.String()[before:]
	}

	// Nothing to say when nothing has ended.
	if got := report(); got != "" {
		t.Fatalf("report with no jobs = %q", got)
	}

	// The report must not disturb the status of the line the user ran, which
	// is the reason it saves and restores $?.
	run("false")
	if got := report(); got != "" {
		t.Fatalf("report after false = %q", got)
	}
	if got := run("echo $?"); got != "1\n" {
		t.Fatalf("status after the report = %q, want \"1\\n\"", got)
	}

	// A finished job is announced, and announced only once.
	run("sleep 0 &")
	run("wait")
	if got := report(); !strings.Contains(got, "Done") {
		t.Fatalf("first report = %q, want a Done line", got)
	}
	if got := report(); got != "" {
		t.Fatalf("second report = %q, want nothing", got)
	}

	// A job still running is not announced. In its own shell: with the
	// version of sh pinned here the finished job above is still in the
	// table, so this one would be [2] and `kill %1` would name a corpse.
	var out2 strings.Builder
	sh2, err := New(nil, strings.NewReader(""), &out2, &out2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sh2.Run(ctx, "sleep 30 &"); err != nil {
		t.Fatal(err)
	}
	before := out2.Len()
	sh2.ReportJobs(ctx)
	if got := out2.String()[before:]; got != "" {
		t.Fatalf("report while a job runs = %q", got)
	}
	_, _ = sh2.Run(ctx, "kill %1; wait") //nolint:errcheck,gosec // cleanup
}
