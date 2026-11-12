package timeline

import (
	"strings"
	"testing"
	"time"
)

func buildFixture(t *testing.T) (*Timeline, []Deploy) {
	t.Helper()
	slack, err := ParseSlack(read(t, "slack.json"))
	if err != nil {
		t.Fatal(err)
	}
	pd, err := ParsePagerDuty(read(t, "pagerduty.json"))
	if err != nil {
		t.Fatal(err)
	}
	all, err := ParseDeploys(read(t, "deploys.json"))
	if err != nil {
		t.Fatal(err)
	}
	deploys := DeploysInWindow(all, append(append([]Event{}, slack...), pd...), 2*time.Hour)
	return Build("Checkout SEV-1", time.UTC, slack, pd, DeployEvents(deploys)), deploys
}

func TestParseDeploys(t *testing.T) {
	d, err := ParseDeploys(read(t, "deploys.json"))
	if err != nil {
		t.Fatalf("ParseDeploys: %v", err)
	}
	if len(d) != 3 {
		t.Fatalf("want 3 deploys, got %d", len(d))
	}
	if d[1].Service != "checkout" || d[1].Range() != "9f8e7d6..abc1234" || d[1].Actor != "Cara Dev" {
		t.Errorf("deploy 1 = %+v", d[1])
	}
	if got := d[1].Time.Format(time.RFC3339); got != "2026-07-17T09:52:00Z" {
		t.Errorf("deploy 1 time = %s", got)
	}
	if _, err := ParseDeploys([]byte(`[{"time":"yesterday"}]`)); err == nil {
		t.Error("want an error for a bad time")
	}
}

func TestDeploysInWindow(t *testing.T) {
	_, deploys := buildFixture(t)
	// The 2h lookback drops the previous day's search deploy.
	if len(deploys) != 2 {
		t.Fatalf("want 2 deploys in window, got %d: %+v", len(deploys), deploys)
	}
	if deploys[0].Version != "v2.14.0" || deploys[1].Version != "v2.13.4" {
		t.Errorf("wrong deploys kept: %+v", deploys)
	}
}

func TestTimeToMitigate(t *testing.T) {
	tl, _ := buildFixture(t)
	// The Slack ts carries microseconds, so compare to the second.
	if tl.TTM.Round(time.Second) != 25*time.Minute {
		t.Errorf("TTM = %s, want 25m0s", tl.TTM)
	}
	if !strings.Contains(tl.Markdown(), "**Time to mitigate (TTM):** 25m00s") {
		t.Error("markdown summary lacks TTM")
	}
}

func TestParticipants(t *testing.T) {
	tl, _ := buildFixture(t)
	got := participants(tl.Events)
	want := []struct {
		name  string
		roles string
		msgs  int
	}{
		{"Cara Dev", "deployed", 0},
		{"Ann Ops", "declared, resolved", 2},
		{"Ben Dev", "acknowledged, deployed, mitigated", 4},
	}
	if len(got) != len(want) {
		t.Fatalf("participants = %+v", got)
	}
	for i, w := range want {
		g := got[i]
		if g.Name != w.name || strings.Join(g.Roles, ", ") != w.roles || g.Messages != w.msgs {
			t.Errorf("participant %d = %+v, want %+v", i, g, w)
		}
	}
	// Datadog triggered the incident as a service agent, so it is not a person.
	for _, p := range got {
		if p.Name == "Datadog" {
			t.Error("service agent listed as a participant")
		}
	}
}

func TestPagerDutyBotAgent(t *testing.T) {
	data := []byte(`[
		{"type":"trigger_log_entry","created_at":"2026-07-17T10:00:00Z","agent":{"type":"service_reference","summary":"API"}},
		{"type":"acknowledge_log_entry","created_at":"2026-07-17T10:01:00Z","agent":{"type":"user_reference","summary":"Ann"}},
		{"type":"annotate_log_entry","created_at":"2026-07-17T10:02:00Z","agent":{"summary":"Ben"}},
		{"type":"resolve_log_entry","created_at":"2026-07-17T10:03:00Z"}
	]`)
	events, err := ParsePagerDuty(data)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range []bool{true, false, false, true} {
		if events[i].Bot != want {
			t.Errorf("entry %d Bot = %v, want %v", i, events[i].Bot, want)
		}
	}
}

func TestSlackPinnedAndBots(t *testing.T) {
	data := []byte(`[
		{"type":"message","ts":"1784282400.000000","text":"Status page: https://status.example.com","user_profile":{"real_name":"Ann Ops"},"pinned_to":["C123"]},
		{"type":"message","subtype":"bot_message","bot_id":"B1","ts":"1784282460.000000","text":"Deploy finished"}
	]`)
	events, err := ParseSlack(data)
	if err != nil {
		t.Fatal(err)
	}
	if !events[0].Pinned || events[0].Bot {
		t.Errorf("event 0 = %+v, want pinned, not bot", events[0])
	}
	if events[1].Pinned || !events[1].Bot {
		t.Errorf("event 1 = %+v, want bot, not pinned", events[1])
	}
	tl := Build("pins", time.UTC, events)
	if !strings.Contains(tl.Table(), "📌") {
		t.Error("pinned message not marked in the table")
	}
	pm := tl.Postmortem(PostmortemOptions{})
	if len(pm.Pinned) != 1 || len(pm.Participants) != 1 {
		t.Errorf("pinned = %d, participants = %+v", len(pm.Pinned), pm.Participants)
	}
	var b strings.Builder
	tmpl, err := ParseTemplate("default", DefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.Render(&b, tmpl); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), "## Pinned messages\n\n- 2026-07-17 10:00:00 Ann Ops: Status page") {
		t.Errorf("pinned section missing:\n%s", b.String())
	}
}

func TestDefaultTemplate(t *testing.T) {
	tl, deploys := buildFixture(t)
	started := time.Date(2026, 7, 17, 9, 55, 0, 0, time.UTC)
	tmpl, err := ParseTemplate("default", DefaultTemplate)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := tl.Postmortem(PostmortemOptions{ImpactStart: started, Deploys: deploys}).Render(&b, tmpl); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{
		"# Checkout SEV-1",
		"| Impact started | 2026-07-17 09:55:00 |",
		"| Time to detect (TTD) | 5m00s |",
		"| Time to acknowledge (MTTA) | 6m00s |",
		"| Time to mitigate (TTM) | 25m00s |",
		"| Time to resolve (MTTR) | 30m00s |",
		"## Impact\n\n<!--",
		"| Ben Dev | acknowledged, deployed, mitigated | 4 |",
		"| 2026-07-17 09:52:00 | checkout | v2.14.0 | 9f8e7d6..abc1234 | Cara Dev |",
		"| Time (UTC) | Source | Event |",
		"## Contributing factors\n\n<!--",
		"## Action items",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("postmortem missing %q\n---\n%s", want, md)
		}
	}
	if strings.Contains(md, "Pinned messages") {
		t.Error("empty pinned section rendered")
	}
}

func TestDefaultTemplateWithoutOptionalInputs(t *testing.T) {
	pd, _ := ParsePagerDuty([]byte(`[{"type":"trigger","created_at":"2026-07-17T10:00:00Z","agent":{"summary":"Ann"}}]`))
	tmpl, _ := ParseTemplate("default", DefaultTemplate)
	var b strings.Builder
	if err := Build("t", time.UTC, pd).Postmortem(PostmortemOptions{}).Render(&b, tmpl); err != nil {
		t.Fatal(err)
	}
	md := b.String()
	for _, want := range []string{
		"| Impact started | _unknown_ |",
		"| Resolved | _not recorded_ |",
		"| Time to detect (TTD) | — |",
		"_No deploys in the window, or no deploy history given (`--deploys`)._",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("postmortem missing %q\n---\n%s", want, md)
		}
	}
}

func TestCustomTemplate(t *testing.T) {
	tl, deploys := buildFixture(t)
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	tl.Loc = ny
	tmpl, err := ParseTemplate("custom", `{{ .Title }} declared {{ time .Summary.Declared }} {{ .TZ }}
{{ range .Participants }}{{ .Name }}={{ join .Roles "+" }};{{ end }}
{{ range .Deploys }}{{ .Service }}@{{ .Range }} {{ end }}
mttr={{ duration .Summary.MTTR }} events={{ len .Events }}`)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if err := tl.Postmortem(PostmortemOptions{Deploys: deploys}).Render(&b, tmpl); err != nil {
		t.Fatal(err)
	}
	want := "Checkout SEV-1 declared 2026-07-17 06:00:00 America/New_York\n" +
		"Cara Dev=deployed;Ann Ops=declared+resolved;Ben Dev=acknowledged+deployed+mitigated;\n" +
		"checkout@9f8e7d6..abc1234 checkout@abc1234..9f8e7d6 \n" +
		"mttr=30m00s events=9"
	if b.String() != want {
		t.Errorf("custom template output:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestTemplateErrors(t *testing.T) {
	if _, err := ParseTemplate("bad", "{{ .Title "); err == nil {
		t.Error("want a parse error")
	}
	tmpl, err := ParseTemplate("missing", "{{ .NoSuchField }}")
	if err != nil {
		t.Fatal(err)
	}
	tl, _ := buildFixture(t)
	var b strings.Builder
	if err := tl.Postmortem(PostmortemOptions{}).Render(&b, tmpl); err == nil {
		t.Error("want an execution error for an unknown field")
	}
}
