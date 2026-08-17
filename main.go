// Command incident-timeline merges a Slack channel export and a PagerDuty
// incident log into one chronological, timezone-normalized Markdown timeline
// ready to paste into a postmortem.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/moveeeax/incident-timeline/internal/timeline"
)

// version is overridden at build time with -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "incident-timeline:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout *os.File) error {
	fs := flag.NewFlagSet("incident-timeline", flag.ContinueOnError)
	var (
		slackPath = fs.String("slack", "", "path to a Slack channel export (JSON)")
		pdPath    = fs.String("pd", "", "path to a PagerDuty incident log (JSON)")
		tzName    = fs.String("tz", "UTC", "display timezone (IANA name, e.g. America/New_York)")
		title     = fs.String("title", "Incident Timeline", "document title")
		outPath   = fs.String("out", "", "write to this file instead of stdout")
		showVer   = fs.Bool("version", false, "print version and exit")
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: incident-timeline --slack export.json --pd incident.json [--tz ZONE] [--out FILE]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVer {
		fmt.Fprintln(stdout, "incident-timeline", version)
		return nil
	}
	if *slackPath == "" && *pdPath == "" {
		fs.Usage()
		return fmt.Errorf("provide at least one of --slack or --pd")
	}

	loc, err := time.LoadLocation(*tzName)
	if err != nil {
		return fmt.Errorf("unknown timezone %q: %w", *tzName, err)
	}

	var sources [][]timeline.Event
	if *slackPath != "" {
		ev, err := loadSlack(*slackPath)
		if err != nil {
			return err
		}
		sources = append(sources, ev)
	}
	if *pdPath != "" {
		ev, err := loadPagerDuty(*pdPath)
		if err != nil {
			return err
		}
		sources = append(sources, ev)
	}

	tl := timeline.Build(*title, loc, sources...)
	md := tl.Markdown()

	if *outPath != "" {
		if err := os.WriteFile(*outPath, []byte(md), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", *outPath, err)
		}
		fmt.Fprintf(os.Stderr, "wrote %d events to %s\n", len(tl.Events), *outPath)
		return nil
	}
	fmt.Fprint(stdout, md)
	return nil
}

func loadSlack(path string) ([]timeline.Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read slack export: %w", err)
	}
	return timeline.ParseSlack(data)
}

func loadPagerDuty(path string) ([]timeline.Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read pagerduty log: %w", err)
	}
	return timeline.ParsePagerDuty(data)
}
