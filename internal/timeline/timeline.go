package timeline

import (
	"sort"
	"time"
)

// Timeline is a merged, chronologically sorted set of events plus the derived
// incident metrics.
type Timeline struct {
	Title  string
	Loc    *time.Location
	Events []Event
	Metrics
}

// Metrics holds the lifecycle timestamps and the two headline SRE numbers.
// Any field may be zero if the corresponding marker never appeared.
type Metrics struct {
	Declared     time.Time
	Acknowledged time.Time
	Mitigated    time.Time
	Resolved     time.Time
	MTTA         time.Duration // acknowledged - declared
	TTM          time.Duration // mitigated - declared
	MTTR         time.Duration // resolved - declared
}

// Build merges events from every source into one timeline, sorts them by time
// (ties broken so PagerDuty state changes sort before chatter), and computes
// metrics. loc is the display timezone; nil means UTC.
func Build(title string, loc *time.Location, sources ...[]Event) *Timeline {
	if loc == nil {
		loc = time.UTC
	}
	var all []Event
	for _, s := range sources {
		all = append(all, s...)
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].Time.Equal(all[j].Time) {
			return all[i].Time.Before(all[j].Time)
		}
		// On an exact tie, a marker event outranks a plain note so the state
		// change is what the reader sees first.
		return markerRank(all[i].Marker) < markerRank(all[j].Marker)
	})
	tl := &Timeline{Title: title, Loc: loc, Events: all}
	tl.Metrics = computeMetrics(all)
	return tl
}

func markerRank(m Marker) int {
	if m == MarkerNone {
		return 1
	}
	return 0
}

// computeMetrics extracts lifecycle timestamps. Declared and Acknowledged take
// the first occurrence; Mitigated and Resolved take the last, because an
// incident can be re-opened before it is finally closed.
func computeMetrics(events []Event) Metrics {
	var m Metrics
	for _, e := range events {
		switch e.Marker {
		case MarkerDeclared:
			if m.Declared.IsZero() {
				m.Declared = e.Time
			}
		case MarkerAcknowledged:
			if m.Acknowledged.IsZero() {
				m.Acknowledged = e.Time
			}
		case MarkerMitigated:
			m.Mitigated = e.Time
		case MarkerResolved:
			m.Resolved = e.Time
		}
	}
	if !m.Declared.IsZero() && !m.Acknowledged.IsZero() && !m.Acknowledged.Before(m.Declared) {
		m.MTTA = m.Acknowledged.Sub(m.Declared)
	}
	if !m.Declared.IsZero() && !m.Mitigated.IsZero() && !m.Mitigated.Before(m.Declared) {
		m.TTM = m.Mitigated.Sub(m.Declared)
	}
	if !m.Declared.IsZero() && !m.Resolved.IsZero() && !m.Resolved.Before(m.Declared) {
		m.MTTR = m.Resolved.Sub(m.Declared)
	}
	return m
}
