// Command incident-timeline merges a Slack channel export and a PagerDuty
// incident log into one chronological, timezone-normalized Markdown timeline
// ready to paste into a postmortem, or renders a postmortem draft from it.
// The "noise" subcommand ranks Prometheus alerts by how noisy they are.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
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

func run(args []string, stdout io.Writer) error {
	if len(args) > 0 && args[0] == "noise" {
		return runNoise(args[1:], stdout)
	}
	fs := flag.NewFlagSet("incident-timeline", flag.ContinueOnError)
	var (
		slackPath   = fs.String("slack", "", "path to a Slack channel export (JSON)")
		pdPath      = fs.String("pd", "", "path to a PagerDuty incident log (JSON)")
		deploysPath = fs.String("deploys", "", "path to a deploy history (JSON array)")
		lookback    = fs.Duration("deploy-lookback", 24*time.Hour, "include deploys this long before the first event")
		tzName      = fs.String("tz", "UTC", "display timezone (IANA name, e.g. America/New_York)")
		title       = fs.String("title", "Incident Timeline", "document title")
		postmortem  = fs.Bool("postmortem", false, "render a postmortem draft with the built-in template")
		tmplPath    = fs.String("template", "", "render a postmortem draft with this Go text/template file (implies --postmortem)")
		printTmpl   = fs.Bool("print-template", false, "print the built-in postmortem template and exit")
		startedStr  = fs.String("started", "", "when impact began (RFC 3339), for time to detect")
		outPath     = fs.String("out", "", "write to this file instead of stdout")
		showVer     = fs.Bool("version", false, "print version and exit")
	)
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: incident-timeline --slack export.json --pd incident.json [--deploys deploys.json] [--postmortem | --template FILE] [--tz ZONE] [--out FILE]\n")
		fmt.Fprintf(os.Stderr, "       incident-timeline noise --help\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVer {
		fmt.Fprintln(stdout, "incident-timeline", version)
		return nil
	}
	if *printTmpl {
		fmt.Fprint(stdout, timeline.DefaultTemplate)
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

	var started time.Time
	if *startedStr != "" {
		started, err = time.Parse(time.RFC3339, *startedStr)
		if err != nil {
			return fmt.Errorf("--started: %w", err)
		}
	}

	// Parse the template before doing any work so a typo fails fast.
	tmplName, tmplText := "default", timeline.DefaultTemplate
	if *tmplPath != "" {
		data, err := os.ReadFile(*tmplPath)
		if err != nil {
			return fmt.Errorf("read template: %w", err)
		}
		tmplName, tmplText = *tmplPath, string(data)
		*postmortem = true
	}
	tmpl, err := timeline.ParseTemplate(tmplName, tmplText)
	if err != nil {
		return err
	}

	var deploys []timeline.Deploy
	if *deploysPath != "" {
		data, err := os.ReadFile(*deploysPath)
		if err != nil {
			return fmt.Errorf("read deploys: %w", err)
		}
		all, err := timeline.ParseDeploys(data)
		if err != nil {
			return err
		}
		var incident []timeline.Event
		for _, s := range sources {
			incident = append(incident, s...)
		}
		deploys = timeline.DeploysInWindow(all, incident, *lookback)
		sources = append(sources, timeline.DeployEvents(deploys))
	}

	tl := timeline.Build(*title, loc, sources...)
	var md string
	if *postmortem {
		var buf bytes.Buffer
		pm := tl.Postmortem(timeline.PostmortemOptions{ImpactStart: started, Deploys: deploys})
		if err := pm.Render(&buf, tmpl); err != nil {
			return err
		}
		md = buf.String()
	} else {
		md = tl.Markdown()
	}

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
