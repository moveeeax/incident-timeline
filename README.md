# incident-timeline

[![ci](https://github.com/moveeeax/incident-timeline/actions/workflows/ci.yml/badge.svg)](https://github.com/moveeeax/incident-timeline/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Reconstruct *what happened when* for a postmortem in seconds. `incident-timeline`
merges a **Slack channel export** and a **PagerDuty incident log** into a single
chronological, timezone-normalized Markdown timeline — with the lifecycle
markers (declared, acknowledged, mitigated, resolved) and MTTA/MTTR already
computed, ready to paste into the postmortem doc.

## Why

After an incident, the hardest part of the writeup is rebuilding the sequence:
Slack has the human chatter, PagerDuty has the machine state changes, and both
are in their own formats and timezones. `incident-timeline` stitches them into
one ordered story so you spend the retro discussing causes, not reassembling
timestamps.

## Install

Requires Go 1.26 or newer.

```sh
go install github.com/moveeeax/incident-timeline@latest
```

Or build from source:

```sh
git clone https://github.com/moveeeax/incident-timeline.git
cd incident-timeline
go build -o incident-timeline .
```

## Usage

```sh
incident-timeline --slack export.json --pd incident.json
```

Flags:

| Flag | Default | Description |
| --- | --- | --- |
| `--slack` | — | path to a Slack channel export (JSON) |
| `--pd` | — | path to a PagerDuty incident log (JSON) |
| `--deploys` | — | path to a deploy history (JSON, see [below](#deploy-history)) |
| `--deploy-lookback` | `24h` | include deploys this long before the first Slack/PagerDuty event |
| `--tz` | `UTC` | display timezone (IANA name, e.g. `America/New_York`) |
| `--title` | `Incident Timeline` | document title |
| `--postmortem` | off | render a postmortem draft with the built-in template |
| `--template` | — | render a postmortem draft with your own Go `text/template` file (implies `--postmortem`) |
| `--print-template` | — | print the built-in postmortem template and exit |
| `--started` | — | when impact began (RFC 3339), used for time to detect |
| `--out` | stdout | write to this file instead of stdout |
| `--version` | — | print version and exit |

At least one of `--slack` / `--pd` is required; give both for the full picture.

### Example

```sh
incident-timeline \
  --slack examples/slack-export.json \
  --pd examples/pagerduty-log.json \
  --title "Checkout SEV-1"
```

produces ([full output](examples/timeline.md)):

```markdown
# Checkout SEV-1

## Summary

- **Declared:** 2026-07-17 10:00:00
- **Acknowledged:** 2026-07-17 10:06:00
- **Mitigated:** 2026-07-17 10:25:00
- **Resolved:** 2026-07-17 10:30:00
- **Time to acknowledge (MTTA):** 6m00s
- **Time to mitigate (TTM):** 25m00s
- **Time to resolve (MTTR):** 30m00s

## Timeline

| Time (UTC) | Source | Event |
| --- | --- | --- |
| 2026-07-17 10:00:00 | pagerduty | 🚨 **Declared** — Checkout error rate > 5% (Datadog) |
| 2026-07-17 10:00:00 | slack | 🚨 **Declared** — Declaring a SEV-1 incident: checkout error rate spiking (Ann Ops) |
| 2026-07-17 10:06:00 | pagerduty | 👀 **Acknowledged** — acknowledge (Ben Dev) |
| ...
```

## Postmortem draft

`--postmortem` turns the timeline into a first draft of the postmortem
document: the summary times and durations, the participants, the deploys in
the window, pinned Slack messages, and the timeline table. Impact,
contributing factors and action items stay empty prompts; the tool never
guesses a cause.

```sh
incident-timeline \
  --slack examples/slack-export.json \
  --pd examples/pagerduty-log.json \
  --deploys examples/deploys.json \
  --started 2026-07-17T09:55:00Z \
  --title "Checkout SEV-1" --postmortem --out postmortems/checkout-sev1.md
```

See [`examples/postmortem.md`](examples/postmortem.md) for the output.

Participants are the named people in the record, in order of first
appearance, with the roles the record implies: `declared`, `acknowledged`,
`mitigated`, `resolved` (from the lifecycle markers of their messages and
PagerDuty entries) and `deployed` (from the deploy history). Slack bot messages
and PagerDuty entries made by a service or integration agent are left out.

Time to detect needs `--started`, because neither Slack nor PagerDuty knows
when impact began. Time to mitigate and time to resolve are measured from the
declaration, like MTTA.

### Your own template

To match the format your team already uses, write a Go
[`text/template`](https://pkg.go.dev/text/template) and pass it with
`--template`. `incident-timeline --print-template` prints the built-in one as a
starting point. The template sees:

| Field | Type | Contents |
| --- | --- | --- |
| `.Title` | string | `--title` |
| `.TZ` | string | display timezone name |
| `.Summary` | struct | `ImpactStart`, `Declared`, `Acknowledged`, `Mitigated`, `Resolved` (times), `TTD`, `MTTA`, `TTM`, `MTTR` (durations); zero when not recorded |
| `.Events` | list | every event: `Time`, `Source`, `Actor`, `Text`, `Marker`, `Bot`, `Pinned` |
| `.Pinned` | list | the pinned Slack messages, same fields as `.Events` |
| `.Participants` | list | `Name`, `Roles` (list), `Messages` (count) |
| `.Deploys` | list | `Time`, `Service`, `Version`, `From`, `To`, `Actor`, `URL`, and `.Range` (`from..to`) |
| `.Timeline` | string | the timeline table, already rendered as Markdown |

All times are already in the `--tz` zone. Helper functions:

- `time` formats a time as `2006-01-02 15:04:05`, or an empty string when it is unset;
- `duration` formats a duration as `1h30m00s`, or an empty string when it is unset;
- `join` is `strings.Join`, e.g. `{{ join .Roles ", " }}`;
- `cell` makes free text safe inside a Markdown table cell.

For example:

```
# {{ .Title }}

Declared {{ time .Summary.Declared }}, resolved after {{ or (duration .Summary.MTTR) "n/a" }}.

{{ range .Participants }}- {{ .Name }} ({{ join .Roles ", " }})
{{ end }}
{{ .Timeline }}
```

### Deploy history

`--deploys` reads a JSON array of deploys. Only `time` is required:

```json
[
  {
    "time": "2026-07-17T09:52:00Z",
    "service": "checkout",
    "version": "v2.14.0",
    "from": "9f8e7d6",
    "to": "abc1234",
    "actor": "Cara Dev",
    "url": "https://ci.example.com/deploys/812"
  }
]
```

Deploys from `--deploy-lookback` before the first Slack/PagerDuty event up to
the last one are kept. They show up in the timeline with source `deploy` and
in the postmortem's deploy table with their commit range.

## Alert noise report

`incident-timeline noise` ranks Prometheus alerts by how noisy they are, to
produce the list of alerts worth deleting or retuning. It reconstructs the
firing history from the `ALERTS` series and computes, per alert:

- fire count, mean and median time to clear;
- the share of firings that cleared in under `--quick` (default 5m);
- flaps: firings that cleared before their rule's `for:` duration, which is the
  window the condition was supposed to survive;
- the busiest 3-hour band of the day, to catch alerts that only fire during a
  nightly batch.

Paging alerts and ticket-only alerts are ranked in separate tables: an alert
pages when its `severity` label is `critical` or `page` (change with
`--severity-label` and `--page-severities`). An alert with at least
`--min-fires` fires becomes a delete candidate when half or more of its
firings cleared under `--quick`, or half or more were flaps. Candidates are
listed first with the evidence for each.

Query Prometheus directly:

```sh
incident-timeline noise --prometheus http://localhost:9090 --since 30d --min-fires 5 --format csv
```

or work from saved API responses, e.g. for a review meeting or offline:

```sh
curl -sG http://localhost:9090/api/v1/query_range \
  --data-urlencode 'query=ALERTS{alertstate="firing"}' \
  --data-urlencode "start=$(( $(date +%s) - 7*86400 ))" \
  --data-urlencode "end=$(date +%s)" \
  --data-urlencode step=60 > alerts.json
curl -s 'http://localhost:9090/api/v1/rules?type=alert' > rules.json

incident-timeline noise --alerts alerts.json --rules rules.json
```

See [`examples/noise-report.md`](examples/noise-report.md) for the Markdown
output.

| Flag | Default | Description |
| --- | --- | --- |
| `--alerts` | — | saved `/api/v1/query_range` response for `ALERTS` (JSON) |
| `--prometheus` | — | Prometheus base URL to query instead |
| `--query` | `ALERTS{alertstate="firing"}` | range query to run with `--prometheus`; add matchers to narrow it |
| `--since` | `30d` | with `--prometheus`, how far back to look (`7d`, `36h`, ...) |
| `--step` | `1m` live, inferred from file | sample resolution |
| `--rules` | fetched live | saved `/api/v1/rules` response, for each alert's `for:` duration |
| `--min-fires` | `5` | fire count below which an alert is never a delete candidate |
| `--quick` | `5m` | a firing shorter than this counts as cleared on its own |
| `--by-labels` | off | rank each label set separately instead of grouping by alertname |
| `--severity-label` | `severity` | label that says whether an alert pages |
| `--page-severities` | `critical,page` | values of that label that page |
| `--format` | `md` | `md` or `csv` |
| `--tz` | `UTC` | timezone for the window and the hour-of-day bands |
| `--timeout` | `2m` | with `--prometheus`, timeout for all requests |
| `--out` | stdout | write to this file instead of stdout |

Limits to keep in mind:

- Prometheus does not record whether anyone acted on an alert, so "cleared
  under `--quick`" stands in for "resolved with no action".
- Durations are accurate to one step. A firing shorter than the step can be
  missed; lower `--step` if your `for:` windows are short.
- Live queries are split into chunks of 10,000 points per series, below the
  11,000-point limit Prometheus enforces on `query_range`.
- Firings cut by the start or end of the window are counted with the part
  inside it.
- Without rules, flaps show `n/a`.

## How it works

1. **Parse.** Each source has its own decoder. Slack messages carry a Unix
   `ts`; PagerDuty log entries carry an RFC 3339 `created_at`. Both are converted
   to UTC on ingest, so nothing downstream has to reason about source timezones.
2. **Classify.** PagerDuty entry types map directly onto lifecycle markers
   (`trigger`→declared, `acknowledge`→acknowledged, `resolve`→resolved). Slack
   free text is matched against a small set of keyword rules for the same
   markers.
3. **Merge & sort.** Every event is folded into one stable-sorted list; on an
   exact-timestamp tie, a state change sorts ahead of chatter.
4. **Render.** The list becomes a Markdown summary block (with MTTA/MTTR) plus a
   chronological table, formatted in the display timezone you pass via `--tz`.

## Supported input formats

- **Slack:** a channel export — a JSON array of message objects with `ts` and
  `text`. Channel join/leave and other management noise is dropped.
- **PagerDuty:** an incident log — either a bare JSON array of log entries or an
  object with a `log_entries` key. `trigger` / `acknowledge` / `resolve` /
  `annotate` entry types are understood.
- **Deploys:** a JSON array of deploys, see [Deploy history](#deploy-history).
- **Prometheus** (for `noise`): `/api/v1/query_range` and `/api/v1/rules`
  responses.

See [`examples/`](examples/) for runnable fixtures.

## Development

```sh
go test ./...    # unit tests, including a 20k-event large-export check
go vet ./...
```

## License

[MIT](LICENSE)
