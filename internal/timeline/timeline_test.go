package timeline

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func read(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}

func TestParseSlack(t *testing.T) {
	events, err := ParseSlack(read(t, "slack.json"))
	if err != nil {
		t.Fatalf("ParseSlack: %v", err)
	}
	// The channel_join line must be dropped.
	if len(events) != 3 {
		t.Fatalf("want 3 events, got %d", len(events))
	}
	if events[0].Marker != MarkerDeclared {
		t.Errorf("first message marker = %q, want declared", events[0].Marker)
	}
	if events[0].Actor != "Ann Ops" {
		t.Errorf("actor = %q, want Ann Ops", events[0].Actor)
	}
	if got := events[0].Time.UTC().Format(time.RFC3339); got != "2026-07-17T10:00:00Z" {
		t.Errorf("first ts = %s, want 2026-07-17T10:00:00Z", got)
	}
	if events[2].Marker != MarkerMitigated {
		t.Errorf("last message marker = %q, want mitigated", events[2].Marker)
	}
}

func TestParsePagerDuty(t *testing.T) {
	events, err := ParsePagerDuty(read(t, "pagerduty.json"))
	if err != nil {
		t.Fatalf("ParsePagerDuty: %v", err)
	}
	want := []Marker{MarkerDeclared, MarkerAcknowledged, MarkerNone, MarkerResolved}
	if len(events) != len(want) {
		t.Fatalf("want %d events, got %d", len(want), len(events))
	}
	for i, m := range want {
		if events[i].Marker != m {
			t.Errorf("event %d marker = %q, want %q", i, events[i].Marker, m)
		}
	}
	if events[2].Text != "Suspect deploy abc123, preparing rollback" {
		t.Errorf("annotation text = %q", events[2].Text)
	}
}

func TestParsePagerDutyBareArray(t *testing.T) {
	data := []byte(`[{"type":"trigger","created_at":"2026-07-17T10:00:00Z","channel":{"summary":"boom"}}]`)
	events, err := ParsePagerDuty(data)
	if err != nil {
		t.Fatalf("ParsePagerDuty bare array: %v", err)
	}
	if len(events) != 1 || events[0].Marker != MarkerDeclared {
		t.Fatalf("bare array parse wrong: %+v", events)
	}
}

func TestBuildMergesAndSorts(t *testing.T) {
	slack, err := ParseSlack(read(t, "slack.json"))
	if err != nil {
		t.Fatal(err)
	}
	pd, err := ParsePagerDuty(read(t, "pagerduty.json"))
	if err != nil {
		t.Fatal(err)
	}
	tl := Build("Checkout SEV-1", time.UTC, slack, pd)

	// Merged and sorted: every event time is non-decreasing.
	for i := 1; i < len(tl.Events); i++ {
		if tl.Events[i].Time.Before(tl.Events[i-1].Time) {
			t.Fatalf("event %d out of order: %s before %s", i,
				tl.Events[i].Time, tl.Events[i-1].Time)
		}
	}

	// Declared is the first trigger/declare; the PagerDuty trigger at 10:00 and
	// the Slack declare at 10:00 tie, both valid.
	if got := tl.Declared.Format(time.RFC3339); got != "2026-07-17T10:00:00Z" {
		t.Errorf("declared = %s", got)
	}
	if got := tl.Acknowledged.Format(time.RFC3339); got != "2026-07-17T10:06:00Z" {
		t.Errorf("acknowledged = %s", got)
	}
	if tl.MTTA != 6*time.Minute {
		t.Errorf("MTTA = %s, want 6m0s", tl.MTTA)
	}
	if tl.MTTR != 30*time.Minute {
		t.Errorf("MTTR = %s, want 30m0s", tl.MTTR)
	}
}

func TestMarkdownOutput(t *testing.T) {
	slack, _ := ParseSlack(read(t, "slack.json"))
	pd, _ := ParsePagerDuty(read(t, "pagerduty.json"))
	md := Build("Checkout SEV-1", time.UTC, slack, pd).Markdown()

	for _, want := range []string{
		"# Checkout SEV-1",
		"**Time to acknowledge (MTTA):** 6m00s",
		"**Time to resolve (MTTR):** 30m00s",
		"| Time (UTC) | Source | Event |",
		"🚨 **Declared**",
		"✅ **Resolved**",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}
}

func TestTimezoneNormalization(t *testing.T) {
	pd, _ := ParsePagerDuty(read(t, "pagerduty.json"))
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	md := Build("tz", ny, pd).Markdown()
	// 10:00Z is 06:00 EDT in July.
	if !strings.Contains(md, "2026-07-17 06:00:00") {
		t.Errorf("expected EDT-normalized 06:00:00 in output:\n%s", md)
	}
}

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		30 * time.Second:            "30s",
		6 * time.Minute:             "6m00s",
		90 * time.Minute:            "1h30m00s",
		2*time.Hour + 5*time.Second: "2h00m05s",
	}
	for d, want := range cases {
		if got := humanDuration(d); got != want {
			t.Errorf("humanDuration(%s) = %q, want %q", d, got, want)
		}
	}
}

// TestLargeExport exercises the "handles large exports" acceptance criterion.
func TestLargeExport(t *testing.T) {
	var b strings.Builder
	b.WriteByte('[')
	const n = 20000
	base := int64(1721210000)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"type":"message","ts":"%d.000000","user":"U","text":"heartbeat %d"}`, base+int64(i), i)
	}
	b.WriteByte(']')

	events, err := ParseSlack([]byte(b.String()))
	if err != nil {
		t.Fatalf("ParseSlack large: %v", err)
	}
	if len(events) != n {
		t.Fatalf("want %d events, got %d", n, len(events))
	}
	tl := Build("big", time.UTC, events)
	for i := 1; i < len(tl.Events); i++ {
		if tl.Events[i].Time.Before(tl.Events[i-1].Time) {
			t.Fatalf("large export not sorted at %d", i)
		}
	}
}
