# Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `yt-dlp failed: ...` at startup / resolving | **PO Token Provider sidecar not reachable** — restream's yt-dlp plugin auto-discovers the provider only if it listens on `127.0.0.1:4416` in the same network namespace. This bites CLI and config-file modes too, not just Docker. | Verify: `curl -fsS http://127.0.0.1:4416/ping`. With Docker Compose both services use `network_mode: host`; running natively, start the provider with `docker run -d --name pot-provider --network host ...`. See [docs/deployment.md](deployment.md). |
| `yt-dlp failed: ...` | **yt-dlp is too old** — distro packages (apt/brew/pacman) can't handle PO-token / n-sig challenges. | `pipx install -U yt-dlp` (or `pip install -U yt-dlp`) and make sure it's in `PATH`. |
| `yt-dlp failed: ...` | Stream isn't actually live (scheduled / ended). | Confirm the channel is genuinely live right now. |
| `ffmpeg exited with error` / `ffmpeg ... exit status 251` / `Connection refused` | **Bilibili RTMP unreachable** — bad key, wrong ingest region, or blocked network path. | Confirm the Bilibili key is valid; try `ffmpeg.transcode: copy` to rule out encoder issues; inspect FFmpeg stderr via `/healthz` (`stderr_tail`) or `--log-level debug`. |
| `ffmpeg ... exit status 251` / handshake fails | **Bilibili stream-key split is wrong** — the whole address was put into `stream_key`, or `?streamname=...` was left in `rtmp_url`. | `rtmp_url` = everything up to `live-bvc/`; `stream_key` = everything after `?` (`streamname=...&key=...&pflag=2`). See [docs/configuration.md](configuration.md). |
| Bilibili channel flickering on/off | Key / `rtmp_url` split is wrong, so the RTMP handshake intermittently fails. | Re-check the split (above); make sure `rtmp_url` ends at `live-bvc/` and `stream_key` starts at `streamname=`. |
| Stream stalls / frequent disconnects | Unstable network or proxy between you and YouTube/Bilibili. | Check network stability; switch to a different proxy line, or prefer TUN-mode VPN (no proxy config needed). |
| Stream stalls / drops (long-running) | Uplink bitrate exceeds what your connection can sustain. | Cap the bitrate with `ffmpeg.maxrate` (e.g. `6M`). Note it only applies when **transcoding** — in `copy` mode the source bitrate is forwarded unchanged. |
| Stream stalls / drops (short, then recovers) | A hung segment fetch wedges FFmpeg. | restream already sets `reconnect` flags and a 10s read timeout; raise `global.health_check_interval` (stall timeout) if slow networks misfire. |
| `healthz` shows `state: backoff` | FFmpeg failed and the pipeline is waiting on the retry/backoff timer. | Read `last_error` and `stderr_tail` from `GET /healthz` for the actual reason, then apply the matching fix above. `backoff` alone means retries are working — see `retry.*` in [docs/configuration.md](configuration.md). |
| Container: `exec: "yt-dlp": executable file not found in $PATH` | Running a foreign base image that lacks yt-dlp. | Use the project's `Dockerfile` (bundles yt-dlp + FFmpeg), or install yt-dlp yourself. |

## General tips

- Start with `--log-level debug` to see per-pipeline detail.
- The FFmpeg stderr tail is surfaced in `GET /healthz` (`stderr_tail`) and in debug logs.
- For frequent disconnects, increase `retry.initial_interval` and `retry.max_interval`.
