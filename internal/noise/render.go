package noise

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

const tsLayout = "2006-01-02 15:04"

// Markdown renders the report for a review meeting: delete candidates with
// their evidence, then one ranked table per class.
func (r *Report) Markdown() string {
	var b strings.Builder
	b.WriteString("# Alert noise report\n\n")
	fmt.Fprintf(&b, "_Window %s to %s (%s), step %s. Durations are accurate to one step. "+
		"Prometheus does not record whether anyone acted on an alert, so \"cleared < %s\" "+
		"is the share of firings that ended that fast on their own._\n\n",
		r.From.In(r.Loc).Format(tsLayout), r.To.In(r.Loc).Format(tsLayout), r.Loc,
		fmtDuration(r.Step), fmtDuration(r.Quick))

	b.WriteString("## Delete candidates\n\n")
	cands := r.Candidates()
	if len(cands) == 0 {
		b.WriteString("_None at the current thresholds._\n")
	}
	for _, a := range cands {
		fmt.Fprintf(&b, "- **%s**%s (%s): %s\n", escape(a.Alert), labelSuffix(a.Labels), a.Class, strings.Join(a.Evidence, "; "))
	}

	for _, c := range []struct {
		class Class
		title string
	}{{ClassPage, "Paging alerts"}, {ClassTicket, "Ticket-only alerts"}} {
		rows := r.Class(c.class)
		b.WriteString("\n## " + c.title + "\n\n")
		if len(rows) == 0 {
			b.WriteString("_None._\n")
			continue
		}
		b.WriteString("| # | Alert | Fires | Mean TTR | Median TTR | Cleared < " + fmtDuration(r.Quick) + " | Flaps | Busiest hours | Candidate |\n")
		b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
		for i, a := range rows {
			flaps := "n/a"
			if a.For > 0 {
				flaps = fmt.Sprintf("%d (for: %s)", a.Flaps, fmtDuration(a.For))
			}
			cand := ""
			if a.Candidate {
				cand = "yes"
			}
			fmt.Fprintf(&b, "| %d | %s%s | %d | %s | %s | %s | %s | %s (%s) | %s |\n",
				i+1, escape(a.Alert), escape(labelSuffix(a.Labels)), a.Fires,
				fmtDuration(a.MeanTTR), fmtDuration(a.MedianTTR), pct(a.QuickShare()),
				flaps, hourBand(a.PeakStart), pct(a.PeakShare), cand)
		}
	}
	return b.String()
}

// WriteCSV writes one row per alert in rank order. Durations are in seconds.
func (r *Report) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	header := []string{"rank", "class", "alert", "labels", "fires", "total_s", "mean_ttr_s", "median_ttr_s",
		"quick_share", "for_s", "flaps", "peak_hours", "peak_share", "candidate", "evidence"}
	if err := cw.Write(header); err != nil {
		return err
	}
	for i, a := range r.Alerts {
		err := cw.Write([]string{
			strconv.Itoa(i + 1),
			string(a.Class),
			a.Alert,
			a.Labels,
			strconv.Itoa(a.Fires),
			seconds(a.Total.Seconds()),
			seconds(a.MeanTTR.Seconds()),
			seconds(a.MedianTTR.Seconds()),
			strconv.FormatFloat(a.QuickShare(), 'f', 3, 64),
			seconds(a.For.Seconds()),
			strconv.Itoa(a.Flaps),
			hourBand(a.PeakStart),
			strconv.FormatFloat(a.PeakShare, 'f', 3, 64),
			strconv.FormatBool(a.Candidate),
			strings.Join(a.Evidence, "; "),
		})
		if err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func seconds(s float64) string { return strconv.FormatFloat(s, 'f', 0, 64) }

func labelSuffix(labels string) string {
	if labels == "" {
		return ""
	}
	return " " + labels
}

func escape(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
