package noise

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Options controls how firings are reconstructed and judged.
type Options struct {
	// Step is the sample resolution of the series. Zero infers it from the
	// smallest gap between samples.
	Step time.Duration
	// Quick is the threshold under which a firing counts as having cleared on
	// its own within minutes.
	Quick time.Duration
	// MinFires is the fire count below which an alert is never a delete
	// candidate, however noisy it looks.
	MinFires int
	// ByLabels groups by the full label set instead of by alertname.
	ByLabels bool
	// SeverityLabel and PageSeverities decide which alerts page: an alert
	// pages when its SeverityLabel value is in PageSeverities.
	SeverityLabel  string
	PageSeverities []string
	// Rules maps alertname to rule definitions, for the "for" duration.
	Rules map[string][]Rule
	Loc   *time.Location
}

// Class separates alerts that page someone from alerts that only ticket.
type Class string

const (
	ClassPage   Class = "page"
	ClassTicket Class = "ticket"
)

// Firing is one continuous firing interval.
type Firing struct {
	Start, End time.Time
}

// Duration is the length of the interval.
func (f Firing) Duration() time.Duration { return f.End.Sub(f.Start) }

// AlertStats is one row of the report.
type AlertStats struct {
	Alert     string
	Labels    string // full label set when grouping by labels
	Class     Class
	Fires     int
	Total     time.Duration
	MeanTTR   time.Duration
	MedianTTR time.Duration
	Quick     int           // firings shorter than Options.Quick
	For       time.Duration // rule "for" duration; zero when unknown
	Flaps     int           // firings shorter than For
	Hours     [24]int       // firing starts per hour of day, display timezone
	PeakStart int           // first hour of the busiest 3-hour band
	PeakShare float64       // share of starts inside that band
	Candidate bool
	Evidence  []string
}

// QuickShare is the share of firings that cleared under the quick threshold.
func (a AlertStats) QuickShare() float64 { return share(a.Quick, a.Fires) }

// FlapShare is the share of firings that cleared inside the "for" window.
func (a AlertStats) FlapShare() float64 { return share(a.Flaps, a.Fires) }

// Report is the ranked result.
type Report struct {
	From, To time.Time
	Step     time.Duration
	Quick    time.Duration
	Loc      *time.Location
	Alerts   []AlertStats // ranked: candidates first, then by fire count
}

// Class filters the ranked list to one class.
func (r *Report) Class(c Class) []AlertStats {
	var out []AlertStats
	for _, a := range r.Alerts {
		if a.Class == c {
			out = append(out, a)
		}
	}
	return out
}

// Candidates returns the delete candidates in rank order.
func (r *Report) Candidates() []AlertStats {
	var out []AlertStats
	for _, a := range r.Alerts {
		if a.Candidate {
			out = append(out, a)
		}
	}
	return out
}

// Thresholds above which an alert with enough fires becomes a delete
// candidate, and above which the busiest 3-hour band is called out.
const (
	candidateShare = 0.5
	peakShare      = 0.6
	peakBand       = 3
)

// Analyze reconstructs firings from the series and ranks the alerts.
func Analyze(series []Series, opts Options) *Report {
	if opts.Loc == nil {
		opts.Loc = time.UTC
	}
	if opts.SeverityLabel == "" {
		opts.SeverityLabel = "severity"
	}
	step := opts.Step
	if step <= 0 {
		step = inferStep(series)
	}
	r := &Report{Step: step, Quick: opts.Quick, Loc: opts.Loc}

	type group struct {
		stats   AlertStats
		firings []Firing
	}
	groups := map[string]*group{}
	var order []string
	for _, s := range series {
		if st, ok := s.Labels["alertstate"]; ok && st != "firing" {
			continue
		}
		name := s.Labels["alertname"]
		if name == "" || len(s.Samples) == 0 {
			continue
		}
		class := ClassTicket
		for _, sev := range opts.PageSeverities {
			if strings.EqualFold(s.Labels[opts.SeverityLabel], sev) {
				class = ClassPage
			}
		}
		labels := ""
		if opts.ByLabels {
			labels = labelKey(withoutMeta(s.Labels))
		}
		key := string(class) + "\x00" + name + "\x00" + labels
		g, ok := groups[key]
		if !ok {
			g = &group{stats: AlertStats{
				Alert:  name,
				Labels: labels,
				Class:  class,
				For:    ruleFor(opts.Rules[name], s.Labels),
			}}
			groups[key] = g
			order = append(order, key)
		}
		fs := firings(s.Samples, step)
		g.firings = append(g.firings, fs...)
		for _, f := range fs {
			if r.From.IsZero() || f.Start.Before(r.From) {
				r.From = f.Start
			}
			if f.End.After(r.To) {
				r.To = f.End
			}
		}
	}

	for _, k := range order {
		g := groups[k]
		a := g.stats
		durs := make([]time.Duration, 0, len(g.firings))
		for _, f := range g.firings {
			d := f.Duration()
			durs = append(durs, d)
			a.Total += d
			if opts.Quick > 0 && d < opts.Quick {
				a.Quick++
			}
			if a.For > 0 && d < a.For {
				a.Flaps++
			}
			a.Hours[f.Start.In(opts.Loc).Hour()]++
		}
		a.Fires = len(g.firings)
		a.MeanTTR = a.Total / time.Duration(a.Fires)
		a.MedianTTR = median(durs)
		a.PeakStart, a.PeakShare = peak(a.Hours, a.Fires)
		judge(&a, opts)
		r.Alerts = append(r.Alerts, a)
	}

	sort.SliceStable(r.Alerts, func(i, j int) bool {
		a, b := r.Alerts[i], r.Alerts[j]
		if a.Candidate != b.Candidate {
			return a.Candidate
		}
		if a.Fires != b.Fires {
			return a.Fires > b.Fires
		}
		if a.QuickShare() != b.QuickShare() {
			return a.QuickShare() > b.QuickShare()
		}
		if a.Alert != b.Alert {
			return a.Alert < b.Alert
		}
		return a.Labels < b.Labels
	})
	return r
}

// judge marks delete candidates and writes the evidence for each row.
func judge(a *AlertStats, opts Options) {
	a.Evidence = append(a.Evidence, fmt.Sprintf("fired %d times, median %s to clear", a.Fires, fmtDuration(a.MedianTTR)))
	if a.Quick > 0 {
		a.Evidence = append(a.Evidence, fmt.Sprintf("%s cleared in under %s", pct(a.QuickShare()), fmtDuration(opts.Quick)))
	}
	if a.Flaps > 0 {
		a.Evidence = append(a.Evidence, fmt.Sprintf("%d cleared inside its for: %s", a.Flaps, fmtDuration(a.For)))
	}
	if a.Fires >= 2 && a.PeakShare >= peakShare {
		a.Evidence = append(a.Evidence, fmt.Sprintf("%s start in %s", pct(a.PeakShare), hourBand(a.PeakStart)))
	}
	a.Candidate = a.Fires >= opts.MinFires &&
		(a.QuickShare() >= candidateShare || a.FlapShare() >= candidateShare)
}

// firings splits sorted-or-not sample timestamps into continuous intervals.
// A gap longer than 1.5 steps ends a firing. Each interval is closed one step
// after its last sample, so durations are accurate to one step.
func firings(samples []time.Time, step time.Duration) []Firing {
	ts := append([]time.Time(nil), samples...)
	sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
	var out []Firing
	maxGap := step + step/2
	for i, t := range ts {
		if i > 0 && t.Equal(ts[i-1]) {
			continue // duplicate from overlapping chunks
		}
		if len(out) == 0 || t.Sub(ts[i-1]) > maxGap {
			out = append(out, Firing{Start: t})
		}
		out[len(out)-1].End = t.Add(step)
	}
	return out
}

// inferStep is the smallest positive gap between consecutive samples of any
// series, or one minute when no series has two samples.
func inferStep(series []Series) time.Duration {
	var step time.Duration
	for _, s := range series {
		ts := append([]time.Time(nil), s.Samples...)
		sort.Slice(ts, func(i, j int) bool { return ts[i].Before(ts[j]) })
		for i := 1; i < len(ts); i++ {
			if d := ts[i].Sub(ts[i-1]); d > 0 && (step == 0 || d < step) {
				step = d
			}
		}
	}
	if step == 0 {
		return time.Minute
	}
	return step
}

// ruleFor picks the "for" duration of the rule whose labels the series
// carries. With several rules of the same name and none matching, the first
// one wins.
func ruleFor(rules []Rule, labels map[string]string) time.Duration {
	for _, r := range rules {
		match := true
		for k, v := range r.Labels {
			if labels[k] != v {
				match = false
				break
			}
		}
		if match {
			return r.For
		}
	}
	if len(rules) > 0 {
		return rules[0].For
	}
	return 0
}

func withoutMeta(l map[string]string) map[string]string {
	out := make(map[string]string, len(l))
	for k, v := range l {
		if k != "__name__" && k != "alertstate" && k != "alertname" {
			out[k] = v
		}
	}
	return out
}

func median(d []time.Duration) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := append([]time.Duration(nil), d...)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

// peak finds the 3-hour band (wrapping past midnight) with the most firing
// starts. On a tie it prefers a band that opens with a firing hour, so starts
// that all fall in one hour report that hour as the band's first.
func peak(hours [24]int, total int) (start int, frac float64) {
	best := -1
	for h := range 24 {
		n := 0
		for i := range peakBand {
			n += hours[(h+i)%24]
		}
		if n > best || (n == best && hours[start] == 0 && hours[h] > 0) {
			best, start = n, h
		}
	}
	return start, share(best, total)
}

// hourBand renders the band starting at start, e.g. "02:00-04:59".
func hourBand(start int) string {
	return fmt.Sprintf("%02d:00-%02d:59", start, (start+peakBand-1)%24)
}

func share(n, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(n) / float64(total)
}

func pct(f float64) string {
	return fmt.Sprintf("%.0f%%", f*100)
}

// fmtDuration prints a duration without trailing zero units: 5m, 1h30m, 45s.
func fmtDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	d = d.Round(time.Second)
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}
