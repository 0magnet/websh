package shell

import (
	"context"
	"strings"
	"testing"
	"time"
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

	// A finished job is announced, and announced only once. As jobs -n does
	// in bash, a report may first announce it as running, once.
	run("sleep 0 &")
	var reports string
	for range 400 {
		if reports += report(); strings.Contains(reports, "Done") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if strings.Count(reports, "Done") != 1 || strings.Count(reports, "Running") > 1 {
		t.Fatalf("reports = %q, want one Done line, after at most one Running", reports)
	}
	if got := report(); got != "" {
		t.Fatalf("second report = %q, want nothing", got)
	}

	// A job still running is announced once, as bash announces a job it starts,
	// and reaping does not touch it.
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
	if got := out2.String()[before:]; !strings.Contains(got, "Running") {
		t.Fatalf("first report while a job runs = %q, want it running", got)
	}
	before = out2.Len()
	sh2.ReportJobs(ctx)
	if got := out2.String()[before:]; got != "" {
		t.Fatalf("second report while a job runs = %q", got)
	}
	_, _ = sh2.Run(ctx, "kill %1; wait") //nolint:errcheck,gosec // cleanup

	// Now that the interpreter reaps, a job that has been reported is gone
	// and the numbering starts again from one.
	run("sleep 0 &")
	run("wait")
	if got := run("jobs"); got != "" {
		t.Fatalf("a job waited for was still listed: %q", got)
	}
	if got := run("sleep 30 & jobs"); !strings.Contains(got, "[1]") {
		t.Fatalf("numbering after reaping = %q", got)
	}
	run("kill %1; wait")
}
