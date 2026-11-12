# Alert noise report

_Window 2026-07-12 02:10 to 2026-07-17 17:04 (UTC), step 1m. Durations are accurate to one step. Prometheus does not record whether anyone acted on an alert, so "cleared < 5m" is the share of firings that ended that fast on their own._

## Delete candidates

- **DiskSpaceLow** (ticket): fired 6 times, median 3m to clear; 100% cleared in under 5m; 100% start in 02:00-04:59
- **HighLatency** (page): fired 5 times, median 4m to clear; 100% cleared in under 5m; 5 cleared inside its for: 10m

## Paging alerts

| # | Alert | Fires | Mean TTR | Median TTR | Cleared < 5m | Flaps | Busiest hours | Candidate |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | HighLatency | 5 | 4m | 4m | 100% | 5 (for: 10m) | 09:00-11:59 (40%) | yes |
| 2 | CheckoutDown | 2 | 40m | 40m | 0% | 0 (for: 5m) | 10:00-12:59 (50%) |  |

## Ticket-only alerts

| # | Alert | Fires | Mean TTR | Median TTR | Cleared < 5m | Flaps | Busiest hours | Candidate |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 1 | DiskSpaceLow | 6 | 3m | 3m | 100% | n/a | 02:00-04:59 (100%) | yes |
