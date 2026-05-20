## Symptom

Limiter's *own* heartbeat counts against the bucket. Under load it
silently throttles its own health checks and we get paged.

## Plan

1. Move heartbeats into a bypass scope.
2. Add a metric so the throttle-self loop shows up in dashboards.

> Spotted by the `paging-the-pager` runbook, ironically.
