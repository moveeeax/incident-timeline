package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moveeeax/incident-timeline/internal/timeline"
)

func runCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := run(args, &out)
	return out.String(), err
}

func TestRunTimelineWithDeploys(t *testing.T) {
	out, err := runCLI(t, "--slack", "testdata/slack.json", "--pd", "testdata/pagerduty.json",
		"--deploys", "testdata/deploys.json", "--deploy-lookback", "1h")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "| 2026-07-17 09:52:00 | deploy | • Deploy checkout v2.14.0 (9f8e7d6..abc1234) (Cara Dev) |") {
		t.Errorf("deploy row missing:\n%s", out)
	}
	if strings.Contains(out, "search v3.2.0") {
		t.Error("deploy outside the lookback window was included")
	}
}

func TestRunPostmortemDefault(t *testing.T) {
	out, err := runCLI(t, "--slack", "testdata/slack.json", "--pd", "testdata/pagerduty.json",
		"--deploys", "testdata/deploys.json", "--postmortem", "--started", "2026-07-17T09:50:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Participants", "| Time to detect (TTD) | 10m00s |", "## Action items"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRunCustomTemplate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pm.md.tmpl")
	if err := os.WriteFile(path, []byte("{{ .Title }}: {{ duration .Summary.MTTA }}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := runCLI(t, "--pd", "testdata/pagerduty.json", "--title", "INC-1", "--template", path)
	if err != nil {
		t.Fatal(err)
	}
	if out != "INC-1: 6m00s\n" {
		t.Errorf("output = %q", out)
	}

	if err := os.WriteFile(path, []byte("{{ .Title "), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := runCLI(t, "--pd", "testdata/pagerduty.json", "--template", path); err == nil {
		t.Error("want an error for a broken template")
	}
}

func TestRunPrintTemplate(t *testing.T) {
	out, err := runCLI(t, "--print-template")
	if err != nil {
		t.Fatal(err)
	}
	if out != timeline.DefaultTemplate {
		t.Error("--print-template does not print the built-in template")
	}
}

func TestRunBadStarted(t *testing.T) {
	if _, err := runCLI(t, "--pd", "testdata/pagerduty.json", "--postmortem", "--started", "noon"); err == nil {
		t.Error("want an error for a bad --started")
	}
}

func TestRunNoise(t *testing.T) {
	out, err := runCLI(t, "noise", "--alerts", "testdata/alerts.json", "--rules", "testdata/rules.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "# Alert noise report") || !strings.Contains(out, "**HighLatency**") {
		t.Errorf("unexpected output:\n%s", out)
	}
	out, err = runCLI(t, "noise", "--alerts", "testdata/alerts.json", "--format", "csv")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "rank,class,alert,") {
		t.Errorf("csv output = %q", out)
	}
}

func TestRunNoiseFlagErrors(t *testing.T) {
	for _, args := range [][]string{
		{"noise"},
		{"noise", "--alerts", "a.json", "--prometheus", "http://localhost:9090"},
		{"noise", "--alerts", "testdata/alerts.json", "--format", "xml"},
		{"noise", "--prometheus", "http://localhost:9090", "--since", "0d"},
	} {
		if _, err := runCLI(t, args...); err == nil {
			t.Errorf("%v: want an error", args)
		}
	}
}

func TestParseSince(t *testing.T) {
	for in, want := range map[string]string{"30d": "720h0m0s", "36h": "36h0m0s", "90m": "1h30m0s"} {
		got, err := parseSince(in)
		if err != nil || got.String() != want {
			t.Errorf("parseSince(%q) = %s, %v; want %s", in, got, err, want)
		}
	}
	for _, in := range []string{"", "d", "-1d", "soon"} {
		if _, err := parseSince(in); err == nil {
			t.Errorf("parseSince(%q): want an error", in)
		}
	}
}
