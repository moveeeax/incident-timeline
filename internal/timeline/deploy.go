package timeline

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Deploy is one entry of the deploy history: what shipped, when, and which
// commit range it covered. Time is stored in UTC.
type Deploy struct {
	Time    time.Time `json:"-"`
	Service string    `json:"service"`
	Version string    `json:"version"`
	From    string    `json:"from"` // previous deployed commit or tag
	To      string    `json:"to"`   // newly deployed commit or tag
	Actor   string    `json:"actor"`
	URL     string    `json:"url"`
}

type deployJSON struct {
	Deploy
	Time string `json:"time"`
}

// ParseDeploys decodes a deploy history file: a JSON array of objects with an
// RFC 3339 "time" and optional "service", "version", "from", "to", "actor" and
// "url" fields. The result is sorted by time.
func ParseDeploys(data []byte) ([]Deploy, error) {
	var raw []deployJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("deploys: parse: %w", err)
	}
	deploys := make([]Deploy, 0, len(raw))
	for i, r := range raw {
		ts, err := time.Parse(time.RFC3339, r.Time)
		if err != nil {
			return nil, fmt.Errorf("deploys: entry %d: bad time %q: %w", i, r.Time, err)
		}
		d := r.Deploy
		d.Time = ts.UTC()
		deploys = append(deploys, d)
	}
	sort.SliceStable(deploys, func(i, j int) bool { return deploys[i].Time.Before(deploys[j].Time) })
	return deploys, nil
}

// Range renders the commit range as "from..to", or whichever end is known.
func (d Deploy) Range() string {
	switch {
	case d.From != "" && d.To != "":
		return d.From + ".." + d.To
	default:
		return d.From + d.To
	}
}

// DeploysInWindow keeps the deploys that happened between lookback before the
// first event and the last event, the window someone reviewing the incident
// would ask about. With no events every deploy is kept.
func DeploysInWindow(deploys []Deploy, events []Event, lookback time.Duration) []Deploy {
	if len(events) == 0 {
		return deploys
	}
	first, last := events[0].Time, events[0].Time
	for _, e := range events {
		if e.Time.Before(first) {
			first = e.Time
		}
		if e.Time.After(last) {
			last = e.Time
		}
	}
	from := first.Add(-lookback)
	var out []Deploy
	for _, d := range deploys {
		if !d.Time.Before(from) && !d.Time.After(last) {
			out = append(out, d)
		}
	}
	return out
}

// DeployEvents turns deploys into timeline events with Source "deploy".
func DeployEvents(deploys []Deploy) []Event {
	events := make([]Event, 0, len(deploys))
	for _, d := range deploys {
		parts := []string{"Deploy"}
		for _, p := range []string{d.Service, d.Version} {
			if p != "" {
				parts = append(parts, p)
			}
		}
		if r := d.Range(); r != "" {
			parts = append(parts, "("+r+")")
		}
		events = append(events, Event{
			Time:   d.Time,
			Source: "deploy",
			Actor:  d.Actor,
			Text:   strings.Join(parts, " "),
		})
	}
	return events
}
