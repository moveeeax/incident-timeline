package timeline

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// pdLogEntry mirrors the fields we read from a PagerDuty incident log entry.
// PagerDuty exports either a bare array of these or an object with a
// "log_entries" key; ParsePagerDuty accepts both.
type pdLogEntry struct {
	Type      string `json:"type"`
	CreatedAt string `json:"created_at"`
	Summary   string `json:"summary"`
	Agent     struct {
		Summary string `json:"summary"`
	} `json:"agent"`
	Channel struct {
		Summary string `json:"summary"`
		Content string `json:"content"`
	} `json:"channel"`
}

type pdEnvelope struct {
	LogEntries []pdLogEntry `json:"log_entries"`
}

// ParsePagerDuty decodes a PagerDuty incident log and returns normalized
// events. Entry types map onto lifecycle markers: trigger→declared,
// acknowledge→acknowledged, resolve→resolved.
func ParsePagerDuty(data []byte) ([]Event, error) {
	entries, err := decodePD(data)
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(entries))
	for i, e := range entries {
		if e.CreatedAt == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("pagerduty: entry %d: bad created_at %q: %w", i, e.CreatedAt, err)
		}
		events = append(events, Event{
			Time:   ts.UTC(),
			Source: "pagerduty",
			Actor:  pdActor(e),
			Text:   pdText(e),
			Marker: pdMarker(e.Type),
		})
	}
	return events, nil
}

func decodePD(data []byte) ([]pdLogEntry, error) {
	trimmed := strings.TrimSpace(string(data))
	if strings.HasPrefix(trimmed, "{") {
		var env pdEnvelope
		if err := json.Unmarshal(data, &env); err != nil {
			return nil, fmt.Errorf("pagerduty: parse log: %w", err)
		}
		return env.LogEntries, nil
	}
	var entries []pdLogEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("pagerduty: parse log: %w", err)
	}
	return entries, nil
}

// pdMarker maps a PagerDuty log-entry type onto a lifecycle marker. It tolerates
// both the API's "*_log_entry" suffix and short forms like "trigger".
func pdMarker(t string) Marker {
	t = strings.ToLower(t)
	switch {
	case strings.HasPrefix(t, "trigger"):
		return MarkerDeclared
	case strings.HasPrefix(t, "acknowledge"):
		return MarkerAcknowledged
	case strings.HasPrefix(t, "resolve"):
		return MarkerResolved
	default:
		return MarkerNone
	}
}

func pdActor(e pdLogEntry) string {
	if e.Agent.Summary != "" {
		return e.Agent.Summary
	}
	return "PagerDuty"
}

// pdText builds a human-readable description for a log entry, preferring an
// annotation's content, then the channel summary, then the entry summary.
func pdText(e pdLogEntry) string {
	switch {
	case e.Channel.Content != "":
		return e.Channel.Content
	case e.Channel.Summary != "":
		return e.Channel.Summary
	case e.Summary != "":
		return e.Summary
	default:
		return strings.TrimSuffix(e.Type, "_log_entry")
	}
}
