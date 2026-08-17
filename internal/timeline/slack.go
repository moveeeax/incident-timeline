package timeline

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// slackMessage mirrors the fields we care about from a Slack channel export.
// A channel export is a JSON array of these objects.
type slackMessage struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	TS      string `json:"ts"`
	User    string `json:"user"`
	Text    string `json:"text"`
	Profile struct {
		RealName string `json:"real_name"`
		Name     string `json:"name"`
	} `json:"user_profile"`
}

var (
	reDeclared  = regexp.MustCompile(`(?i)declar|incident start|:rotating_light:|\bpaged\b|triggered|sev[- ]?[012]`)
	reMitigated = regexp.MustCompile(`(?i)mitigat|workaround|fail(ed )?over|rolled back|roll ?back|recover`)
	reResolved  = regexp.MustCompile(`(?i)resolv|all clear|all-clear|back to normal|incident over|closing (the )?incident`)
)

// noise lists Slack message subtypes that never belong on an incident timeline.
var noise = map[string]bool{
	"channel_join":    true,
	"channel_leave":   true,
	"channel_topic":   true,
	"channel_purpose": true,
	"bot_add":         true,
	"bot_remove":      true,
}

// ParseSlack decodes a Slack channel export and returns normalized events.
// Messages are timezone-normalized to UTC via their epoch `ts`. Join/leave and
// other channel-management noise is dropped.
func ParseSlack(data []byte) ([]Event, error) {
	var msgs []slackMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		return nil, fmt.Errorf("slack: parse export: %w", err)
	}
	events := make([]Event, 0, len(msgs))
	for i, m := range msgs {
		if m.Type != "" && m.Type != "message" {
			continue
		}
		if noise[m.Subtype] {
			continue
		}
		text := strings.TrimSpace(m.Text)
		if text == "" {
			continue
		}
		ts, err := parseSlackTS(m.TS)
		if err != nil {
			return nil, fmt.Errorf("slack: message %d: %w", i, err)
		}
		events = append(events, Event{
			Time:   ts,
			Source: "slack",
			Actor:  slackActor(m),
			Text:   text,
			Marker: classify(text),
		})
	}
	return events, nil
}

// parseSlackTS turns a Slack timestamp ("1690800000.000100") into a UTC time.
func parseSlackTS(ts string) (time.Time, error) {
	ts = strings.TrimSpace(ts)
	if ts == "" {
		return time.Time{}, fmt.Errorf("empty ts")
	}
	secStr, fracStr, _ := strings.Cut(ts, ".")
	sec, err := strconv.ParseInt(secStr, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("bad ts %q: %w", ts, err)
	}
	var nsec int64
	if fracStr != "" {
		// Slack fractions are microseconds (6 digits); pad/truncate to nanos.
		fracStr = (fracStr + "000000000")[:9]
		nsec, _ = strconv.ParseInt(fracStr, 10, 64)
	}
	return time.Unix(sec, nsec).UTC(), nil
}

func slackActor(m slackMessage) string {
	switch {
	case m.Profile.RealName != "":
		return m.Profile.RealName
	case m.Profile.Name != "":
		return m.Profile.Name
	case m.User != "":
		return m.User
	default:
		return "unknown"
	}
}

// classify maps free text onto a lifecycle marker. Order matters: resolved and
// mitigated win over a generic "declared" keyword if both are present, because
// the closing state is what a reader scans for.
func classify(text string) Marker {
	switch {
	case reResolved.MatchString(text):
		return MarkerResolved
	case reMitigated.MatchString(text):
		return MarkerMitigated
	case reDeclared.MatchString(text):
		return MarkerDeclared
	default:
		return MarkerNone
	}
}
