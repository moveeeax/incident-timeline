# incident-timeline

> Reconstruct 'what happened when' for postmortems in seconds.

**Status:** 🚧 In development

## Overview

Build an incident timeline from Slack and PagerDuty exports.

## Features

- Ingests Slack channel export + PagerDuty incident log
- Merges events into a single chronological timeline
- Detects key markers (declared, mitigated, resolved)
- Outputs Markdown timeline for the postmortem doc
- Timezone normalization

## Stack

Go; JSON parsers for Slack/PagerDuty exports.

## Usage

```bash
incident-timeline --slack export.json --pd incident.json
```

## License

MIT
