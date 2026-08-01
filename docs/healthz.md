# Health endpoint (`/healthz`)

`global.http_addr` (default `:8080`) starts an HTTP server that serves `GET /healthz` with per-pipeline JSON status — handy for 7×24 monitoring and alerting. Set `http_addr` to an empty string to disable it.

```bash
curl -s http://127.0.0.1:8080/healthz
```

Sample response:

```json
{
  "youtube-to-bilibili": {
    "state": "running",
    "started": "2026-08-01T12:00:00Z",
    "uptime": 3600000000000,
    "bitrate": "4561.0kbits/s",
    "fps": 30,
    "stderr_tail": []
  }
}
```

## JSON fields

The response is a map keyed by **pipeline name**, one entry per configured pipeline:

| Field | Type | Description |
|---|---|---|
| `state` | string | Pipeline lifecycle phase (see below). |
| `started` | RFC3339 timestamp | When this pipeline (re)started. |
| `uptime` | integer (nanoseconds) | How long the current run has been up. |
| `last_error` | string | Most recent failure reason (omitted when empty). |
| `bitrate` | string | Current FFmpeg output bitrate, e.g. `4561.0kbits/s` (omitted when empty). |
| `fps` | number | Current output frame rate (omitted when empty). |
| `stderr_tail` | array of strings | Last ~20 lines of FFmpeg stderr for diagnostics (omitted when empty). |

## State values

| State | Meaning |
|---|---|
| `resolving` | Resolving the source stream (yt-dlp extraction in progress). |
| `running` | FFmpeg is actively pulling and pushing. |
| `backoff` | FFmpeg failed; waiting on the retry/backoff timer. |
| `stopped` | Exited cleanly, or not started. |

## Using it for monitoring / alerting

- Alert when `state` is not `running` for a pipeline.
- Use `last_error` and `stderr_tail` for the reason (see [docs/troubleshooting.md](troubleshooting.md)).
- Use `bitrate` / `fps` to detect a degraded or stalled push.
- With Docker, the compose file's healthcheck probes `http://127.0.0.1:8080/healthz` (works because both services use `network_mode: host`).
