package noise

import (
	"context"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
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

func fixtureReport(t *testing.T, opts Options) *Report {
	t.Helper()
	series, err := ParseRange(read(t, "alerts.json"))
	if err != nil {
		t.Fatalf("ParseRange: %v", err)
	}
	rules, err := ParseRules(read(t, "rules.json"))
	if err != nil {
		t.Fatalf("ParseRules: %v", err)
	}
	if opts.Rules == nil {
		opts.Rules = rules
	}
	if opts.Quick == 0 {
		opts.Quick = 5 * time.Minute
	}
	if opts.MinFires == 0 {
		opts.MinFires = 5
	}
	if opts.PageSeverities == nil {
		opts.PageSeverities = []string{"critical", "page"}
	}
	return Analyze(series, opts)
}

func find(t *testing.T, r *Report, name string) AlertStats {
	t.Helper()
	for _, a := range r.Alerts {
		if a.Alert == name {
			return a
		}
	}
	t.Fatalf("alert %s not in report", name)
	return AlertStats{}
}

func TestParseRange(t *testing.T) {
	series, err := ParseRange(read(t, "alerts.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 4 {
		t.Fatalf("want 4 series, got %d", len(series))
	}
	if series[0].Labels["alertname"] != "DiskSpaceLow" || len(series[0].Samples) != 18 {
		t.Errorf("series 0 = %v with %d samples", series[0].Labels, len(series[0].Samples))
	}
	if _, err := ParseRange([]byte(`{"status":"success","data":{"resultType":"vector","result":[]}}`)); err == nil {
		t.Error("want an error for a vector result")
	}
	if _, err := ParseRange([]byte(`{"status":"error","errorType":"bad_data","error":"parse error"}`)); err == nil ||
		!strings.Contains(err.Error(), "parse error") {
		t.Errorf("want the Prometheus error passed through, got %v", err)
	}
}

func TestParseRules(t *testing.T) {
	rules, err := ParseRules(read(t, "rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("want 2 alerting rules (recording rule skipped), got %v", rules)
	}
	if got := rules["HighLatency"][0].For; got != 10*time.Minute {
		t.Errorf("HighLatency for = %s, want 10m", got)
	}
}

func TestFirings(t *testing.T) {
	base := time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)
	at := func(m int) time.Time { return base.Add(time.Duration(m) * time.Minute) }
	// 0,1,2 then a gap, then 10 and 11 out of order with a duplicate.
	got := firings([]time.Time{at(0), at(1), at(2), at(11), at(10), at(11)}, time.Minute)
	if len(got) != 2 {
		t.Fatalf("want 2 firings, got %+v", got)
	}
	if got[0].Duration() != 3*time.Minute || got[1].Duration() != 2*time.Minute {
		t.Errorf("durations = %s, %s", got[0].Duration(), got[1].Duration())
	}
	if !got[1].Start.Equal(at(10)) {
		t.Errorf("second firing starts %s", got[1].Start)
	}
}

func TestAnalyze(t *testing.T) {
	r := fixtureReport(t, Options{})
	if r.Step != time.Minute {
		t.Errorf("inferred step = %s, want 1m", r.Step)
	}
	if len(r.Alerts) != 3 {
		t.Fatalf("want 3 alerts (pending series ignored), got %d", len(r.Alerts))
	}

	disk := find(t, r, "DiskSpaceLow")
	if disk.Class != ClassTicket || disk.Fires != 6 || disk.MedianTTR != 3*time.Minute || disk.QuickShare() != 1 {
		t.Errorf("DiskSpaceLow = %+v", disk)
	}
	if disk.PeakStart != 2 || disk.PeakShare != 1 {
		t.Errorf("DiskSpaceLow peak = %d (%.2f), want 02 (1.00)", disk.PeakStart, disk.PeakShare)
	}
	if disk.For != 0 || disk.Flaps != 0 || !disk.Candidate {
		t.Errorf("DiskSpaceLow for/flaps/candidate = %s/%d/%v", disk.For, disk.Flaps, disk.Candidate)
	}

	lat := find(t, r, "HighLatency")
	if lat.Class != ClassPage || lat.Fires != 5 || lat.For != 10*time.Minute || lat.Flaps != 5 || !lat.Candidate {
		t.Errorf("HighLatency = %+v", lat)
	}

	down := find(t, r, "CheckoutDown")
	if down.Fires != 2 || down.MeanTTR != 40*time.Minute || down.Flaps != 0 || down.Candidate {
		t.Errorf("CheckoutDown = %+v", down)
	}

	// Candidates rank first, then by fire count.
	if r.Alerts[0].Alert != "DiskSpaceLow" || r.Alerts[2].Alert != "CheckoutDown" {
		t.Errorf("rank order = %s, %s, %s", r.Alerts[0].Alert, r.Alerts[1].Alert, r.Alerts[2].Alert)
	}
}

func TestAnalyzeMinFiresAndTimezone(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	r := fixtureReport(t, Options{MinFires: 6, Loc: berlin})
	if find(t, r, "HighLatency").Candidate {
		t.Error("HighLatency fired 5 times and should be below --min-fires 6")
	}
	// 02:10 UTC is 04:10 CEST.
	if got := find(t, r, "DiskSpaceLow").PeakStart; got != 4 {
		t.Errorf("Berlin peak start = %d, want 4", got)
	}
}

func TestAnalyzeByLabels(t *testing.T) {
	series := []Series{
		{Labels: map[string]string{"alertname": "Up", "instance": "a", "severity": "page"}, Samples: []time.Time{time.Unix(0, 0)}},
		{Labels: map[string]string{"alertname": "Up", "instance": "b", "severity": "page"}, Samples: []time.Time{time.Unix(600, 0)}},
	}
	opts := Options{Step: time.Minute, PageSeverities: []string{"page"}}
	if got := len(Analyze(series, opts).Alerts); got != 1 {
		t.Errorf("by alertname: %d rows, want 1", got)
	}
	opts.ByLabels = true
	r := Analyze(series, opts)
	if len(r.Alerts) != 2 || r.Alerts[0].Labels != `{instance="a", severity="page"}` {
		t.Errorf("by labels: %+v", r.Alerts)
	}
}

func TestMarkdownAndCSV(t *testing.T) {
	r := fixtureReport(t, Options{})
	md := r.Markdown()
	for _, want := range []string{
		"## Delete candidates",
		"- **DiskSpaceLow** (ticket): fired 6 times, median 3m to clear; 100% cleared in under 5m; 100% start in 02:00-04:59",
		"5 cleared inside its for: 10m",
		"## Paging alerts",
		"| 2 | CheckoutDown | 2 | 40m | 40m | 0% | 0 (for: 5m) |",
		"## Ticket-only alerts",
		"| 1 | DiskSpaceLow | 6 | 3m | 3m | 100% | n/a | 02:00-04:59 (100%) | yes |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q\n---\n%s", want, md)
		}
	}

	var b strings.Builder
	if err := r.WriteCSV(&b); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(b.String())).ReadAll()
	if err != nil {
		t.Fatalf("CSV does not parse: %v", err)
	}
	if len(rows) != 4 || rows[0][2] != "alert" || rows[1][2] != "DiskSpaceLow" || rows[1][13] != "true" {
		t.Errorf("csv rows = %v", rows)
	}
	if rows[2][9] != "600" {
		t.Errorf("HighLatency for_s = %s, want 600", rows[2][9])
	}
}

func TestClientQueryRangeChunks(t *testing.T) {
	step := time.Minute
	start := time.Unix(1_784_000_000, 0).UTC()
	end := start.Add(step * (maxPoints*2 + 5)) // needs three chunks

	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/query_range":
			q := r.URL.Query()
			calls = append(calls, q.Get("start")+".."+q.Get("end"))
			if q.Get("query") != DefaultQuery || q.Get("step") != "60" {
				t.Errorf("query params = %v", q)
			}
			s, _ := strconv.ParseFloat(q.Get("start"), 64)
			e, _ := strconv.ParseFloat(q.Get("end"), 64)
			if n := int((e-s)/60) + 1; n > maxPoints {
				t.Errorf("chunk has %d points, over %d", n, maxPoints)
			}
			// Every chunk reports the same series firing at its first and
			// last step, so the merge must stitch one label set together.
			w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
				`{"metric":{"alertname":"A","alertstate":"firing"},"values":[[` + q.Get("start") + `,"1"],[` + q.Get("end") + `,"1"]]}]}}`))
		case "/api/v1/rules":
			if r.URL.Query().Get("type") != "alert" {
				t.Errorf("rules type = %q", r.URL.Query().Get("type"))
			}
			w.Write(read(t, "rules.json"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL + "/", HTTP: srv.Client()}
	series, err := c.QueryRange(context.Background(), DefaultQuery, start, end, step)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) != 3 {
		t.Errorf("want 3 chunked calls, got %v", calls)
	}
	if len(series) != 1 || len(series[0].Samples) != 6 {
		t.Fatalf("merged series = %+v", series)
	}
	rules, err := c.Rules(context.Background())
	if err != nil || len(rules) != 2 {
		t.Errorf("Rules = %v, %v", rules, err)
	}
}

func TestClientError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"status":"error","errorType":"bad_data","error":"invalid parameter \"query\""}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL}
	_, err := c.QueryRange(context.Background(), "ALERTS{", time.Unix(0, 0), time.Unix(600, 0), time.Minute)
	if err == nil || !strings.Contains(err.Error(), "invalid parameter") {
		t.Errorf("want the Prometheus error message, got %v", err)
	}
}
