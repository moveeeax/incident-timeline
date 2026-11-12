// Package timeline reconstructs a single chronological incident timeline from
// heterogeneous sources (Slack channel exports and PagerDuty incident logs) and
// renders it as Markdown for a postmortem document.
package timeline

import "time"

// Marker classifies an event against the incident lifecycle. Most events carry
// MarkerNone; the ones that matter for MTTA/MTTR are Declared, Acknowledged,
// Mitigated and Resolved.
type Marker string

const (
	MarkerNone         Marker = ""
	MarkerDeclared     Marker = "declared"
	MarkerAcknowledged Marker = "acknowledged"
	MarkerMitigated    Marker = "mitigated"
	MarkerResolved     Marker = "resolved"
)

// emoji returns a small glyph used when rendering the marker in Markdown.
func (m Marker) emoji() string {
	switch m {
	case MarkerDeclared:
		return "🚨"
	case MarkerAcknowledged:
		return "👀"
	case MarkerMitigated:
		return "🛠"
	case MarkerResolved:
		return "✅"
	default:
		return "•"
	}
}

// title renders the marker for a table cell, e.g. "**Declared**".
func (m Marker) title() string {
	switch m {
	case MarkerDeclared:
		return "**Declared**"
	case MarkerAcknowledged:
		return "**Acknowledged**"
	case MarkerMitigated:
		return "**Mitigated**"
	case MarkerResolved:
		return "**Resolved**"
	default:
		return ""
	}
}

// Event is one normalized entry on the timeline. Time is always stored in UTC;
// display-timezone conversion happens at render time.
type Event struct {
	Time   time.Time
	Source string // "slack", "pagerduty" or "deploy"
	Actor  string
	Text   string
	Marker Marker
	// Bot is true when the actor is an integration or bot rather than a
	// person, so it is left out of the participant list.
	Bot bool
	// Pinned is true for Slack messages pinned in the incident channel.
	Pinned bool
}
