package timeline

import (
	_ "embed"
	"fmt"
	"io"
	"slices"
	"strings"
	"text/template"
	"time"
)

// DefaultTemplate is the built-in postmortem template. It fills in only what
// the inputs record and leaves impact, contributing factors and action items
// as empty prompts.
//
//go:embed postmortem.md.tmpl
var DefaultTemplate string

// Postmortem is the data handed to a postmortem template. All times are
// already converted to the display timezone.
type Postmortem struct {
	Title        string
	TZ           string
	Summary      Summary
	Events       []Event
	Pinned       []Event // Slack messages pinned in the incident channel
	Participants []Participant
	Deploys      []Deploy
	// Timeline is the chronological event table, pre-rendered as Markdown.
	Timeline string
}

// Summary holds the lifecycle timestamps and the durations derived from them.
// Zero values mean the inputs did not record that point.
type Summary struct {
	ImpactStart  time.Time // from --started; the inputs cannot know it
	Declared     time.Time
	Acknowledged time.Time
	Mitigated    time.Time
	Resolved     time.Time
	TTD          time.Duration // declared - impact start
	MTTA         time.Duration // acknowledged - declared
	TTM          time.Duration // mitigated - declared
	MTTR         time.Duration // resolved - declared
}

// Participant is a person who appears in the record, with the roles the record
// implies: declared, acknowledged, mitigated, resolved, deployed.
type Participant struct {
	Name     string
	Roles    []string
	Messages int // Slack messages and PagerDuty entries by this person
}

// PostmortemOptions carries inputs that are not part of the event stream.
type PostmortemOptions struct {
	ImpactStart time.Time
	Deploys     []Deploy
}

// Postmortem collects the template data for this timeline.
func (t *Timeline) Postmortem(opts PostmortemOptions) *Postmortem {
	in := func(ts time.Time) time.Time {
		if ts.IsZero() {
			return ts
		}
		return ts.In(t.Loc)
	}
	p := &Postmortem{
		Title: t.Title,
		TZ:    t.Loc.String(),
		Summary: Summary{
			ImpactStart:  in(opts.ImpactStart),
			Declared:     in(t.Declared),
			Acknowledged: in(t.Acknowledged),
			Mitigated:    in(t.Mitigated),
			Resolved:     in(t.Resolved),
			MTTA:         t.MTTA,
			TTM:          t.TTM,
			MTTR:         t.MTTR,
		},
		Participants: participants(t.Events),
		Timeline:     t.Table(),
	}
	if !opts.ImpactStart.IsZero() && !t.Declared.IsZero() && !t.Declared.Before(opts.ImpactStart) {
		p.Summary.TTD = t.Declared.Sub(opts.ImpactStart)
	}
	for _, e := range t.Events {
		e.Time = in(e.Time)
		p.Events = append(p.Events, e)
		if e.Pinned {
			p.Pinned = append(p.Pinned, e)
		}
	}
	for _, d := range opts.Deploys {
		d.Time = in(d.Time)
		p.Deploys = append(p.Deploys, d)
	}
	return p
}

var markerRole = map[Marker]string{
	MarkerDeclared:     "declared",
	MarkerAcknowledged: "acknowledged",
	MarkerMitigated:    "mitigated",
	MarkerResolved:     "resolved",
}

// participants lists the people in the record in order of first appearance.
// Bots, integrations and unnamed actors are skipped.
func participants(events []Event) []Participant {
	var out []Participant
	index := map[string]int{}
	for _, e := range events {
		if e.Bot || e.Actor == "" || e.Actor == "unknown" {
			continue
		}
		i, ok := index[e.Actor]
		if !ok {
			i = len(out)
			index[e.Actor] = i
			out = append(out, Participant{Name: e.Actor})
		}
		p := &out[i]
		role := markerRole[e.Marker]
		if e.Source == "deploy" {
			role = "deployed"
		} else {
			p.Messages++
		}
		if role != "" && !slices.Contains(p.Roles, role) {
			p.Roles = append(p.Roles, role)
		}
	}
	return out
}

// templateFuncs are available to every postmortem template.
var templateFuncs = template.FuncMap{
	// time formats a timestamp as "2006-01-02 15:04:05", or "" when unset.
	"time": func(ts time.Time) string {
		if ts.IsZero() {
			return ""
		}
		return ts.Format(tsLayout)
	},
	// duration formats a duration as e.g. "1h30m00s", or "" when unset.
	"duration": func(d time.Duration) string {
		if d <= 0 {
			return ""
		}
		return humanDuration(d)
	},
	"join": strings.Join,
	// cell makes free text safe for a Markdown table cell.
	"cell": escapeCell,
}

// ParseTemplate parses a postmortem template with the helper functions
// (time, duration, join, cell) registered.
func ParseTemplate(name, text string) (*template.Template, error) {
	tmpl, err := template.New(name).Funcs(templateFuncs).Parse(text)
	if err != nil {
		return nil, fmt.Errorf("template %s: %w", name, err)
	}
	return tmpl, nil
}

// Render executes tmpl against the postmortem data.
func (p *Postmortem) Render(w io.Writer, tmpl *template.Template) error {
	if err := tmpl.Execute(w, p); err != nil {
		return fmt.Errorf("template %s: %w", tmpl.Name(), err)
	}
	return nil
}
