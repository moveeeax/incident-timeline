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
| `--tz` | `UTC` | display timezone (IANA name, e.g. `America/New_York`) |
| `--title` | `Incident Timeline` | document title |
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
- **Time to resolve (MTTR):** 30m00s

## Timeline

| Time (UTC) | Source | Event |
| --- | --- | --- |
| 2026-07-17 10:00:00 | pagerduty | 🚨 **Declared** — Checkout error rate > 5% (Datadog) |
| 2026-07-17 10:00:00 | slack | 🚨 **Declared** — Declaring a SEV-1 incident: checkout error rate spiking (Ann Ops) |
| 2026-07-17 10:06:00 | pagerduty | 👀 **Acknowledged** — acknowledge (Ben Dev) |
| ...
```

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

See [`examples/`](examples/) for runnable fixtures.

## Development

```sh
go test ./...    # unit tests, including a 20k-event large-export check
go vet ./...
```

## License

[MIT](LICENSE)
