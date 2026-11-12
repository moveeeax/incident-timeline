package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/moveeeax/incident-timeline/internal/noise"
)

// runNoise implements "incident-timeline noise": rank alerts by fire count,
// time to clear and how often they cleared on their own.
func runNoise(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("incident-timeline noise", flag.ContinueOnError)
	var (
		alertsPath = fs.String("alerts", "", "saved Prometheus query_range response for ALERTS (JSON)")
		promURL    = fs.String("prometheus", "", "Prometheus base URL to query live, e.g. http://localhost:9090")
		query      = fs.String("query", noise.DefaultQuery, "range query to run with --prometheus")
		sinceStr   = fs.String("since", "30d", "with --prometheus, how far back to look (e.g. 7d, 36h)")
		step       = fs.Duration("step", 0, "sample resolution (default 1m with --prometheus, inferred from --alerts)")
		rulesPath  = fs.String("rules", "", "saved Prometheus /api/v1/rules response (JSON), for each alert's for: duration")
		minFires   = fs.Int("min-fires", 5, "fire count below which an alert is never a delete candidate")
		quick      = fs.Duration("quick", 5*time.Minute, "a firing shorter than this counts as cleared on its own")
		byLabels   = fs.Bool("by-labels", false, "rank each label set separately instead of grouping by alertname")
		sevLabel   = fs.String("severity-label", "severity", "label that says whether an alert pages")
		pageSev    = fs.String("page-severities", "critical,page", "comma-separated values of --severity-label that page")
		format     = fs.String("format", "md", "output format: md or csv")
		tzName     = fs.String("tz", "UTC", "timezone for the hour-of-day buckets and the window")
		outPath    = fs.String("out", "", "write to this file instead of stdout")
		timeout    = fs.Duration("timeout", 2*time.Minute, "with --prometheus, timeout for all requests")
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: incident-timeline noise (--alerts alerts.json [--rules rules.json] | --prometheus URL [--since 30d]) [--format md|csv] [--out FILE]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if (*alertsPath == "") == (*promURL == "") {
		fs.Usage()
		return fmt.Errorf("provide exactly one of --alerts or --prometheus")
	}
	if *format != "md" && *format != "csv" {
		return fmt.Errorf("unknown --format %q (want md or csv)", *format)
	}
	loc, err := time.LoadLocation(*tzName)
	if err != nil {
		return fmt.Errorf("unknown timezone %q: %w", *tzName, err)
	}

	opts := noise.Options{
		Step:          *step,
		Quick:         *quick,
		MinFires:      *minFires,
		ByLabels:      *byLabels,
		SeverityLabel: *sevLabel,
		Loc:           loc,
	}
	for _, s := range strings.Split(*pageSev, ",") {
		if s = strings.TrimSpace(s); s != "" {
			opts.PageSeverities = append(opts.PageSeverities, s)
		}
	}
	if *rulesPath != "" {
		data, err := os.ReadFile(*rulesPath)
		if err != nil {
			return fmt.Errorf("read rules: %w", err)
		}
		if opts.Rules, err = noise.ParseRules(data); err != nil {
			return err
		}
	}

	var (
		series   []noise.Series
		from, to time.Time
	)
	if *alertsPath != "" {
		data, err := os.ReadFile(*alertsPath)
		if err != nil {
			return fmt.Errorf("read alerts: %w", err)
		}
		if series, err = noise.ParseRange(data); err != nil {
			return err
		}
	} else {
		since, err := parseSince(*sinceStr)
		if err != nil {
			return err
		}
		if opts.Step <= 0 {
			opts.Step = time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		c := &noise.Client{BaseURL: *promURL, HTTP: http.DefaultClient}
		to = time.Now().UTC().Truncate(opts.Step)
		from = to.Add(-since)
		if series, err = c.QueryRange(ctx, *query, from, to, opts.Step); err != nil {
			return err
		}
		if opts.Rules == nil {
			if opts.Rules, err = c.Rules(ctx); err != nil {
				return err
			}
		}
	}

	rep := noise.Analyze(series, opts)
	if !from.IsZero() {
		rep.From, rep.To = from, to
	}

	var buf bytes.Buffer
	if *format == "csv" {
		if err := rep.WriteCSV(&buf); err != nil {
			return err
		}
	} else {
		buf.WriteString(rep.Markdown())
	}
	if *outPath != "" {
		if err := os.WriteFile(*outPath, buf.Bytes(), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", *outPath, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %d alerts to %s\n", len(rep.Alerts), *outPath)
		return nil
	}
	_, err = stdout.Write(buf.Bytes())
	return err
}

// parseSince accepts Go durations plus a whole-day suffix: "30d", "36h".
func parseSince(s string) (time.Duration, error) {
	if days, ok := strings.CutSuffix(s, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("--since %q: want a positive number of days like 30d", s)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("--since %q: want a positive duration like 30d or 36h", s)
	}
	return d, nil
}
