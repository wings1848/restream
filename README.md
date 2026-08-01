# restream — YouTube live → Bilibili relay

[![CI](https://img.shields.io/github/actions/workflow/status/wings1848/restream/ci.yml?style=flat&label=CI&color=blue)](https://github.com/wings1848/restream/actions)
[![Go](https://img.shields.io/github/go-mod/go-version/wings1848/restream?style=flat&label=Go&color=blue)](https://go.dev/)
[![License](https://img.shields.io/github/license/wings1848/restream?style=flat&label=License&color=blue)](LICENSE)
[![Release](https://img.shields.io/github/v/release/wings1848/restream?style=flat&label=Release&color=blue)](https://github.com/wings1848/restream/releases)
[![中文文档](https://img.shields.io/badge/中文文档-README--zh-blue?style=flat&color=blue)](README-zh.md)

A lightweight, unattended live-stream **mirror / relay**: it pulls a YouTube live stream via `yt-dlp` (HLS), optionally transcodes it, and pushes it to Bilibili (RTMP). Built to run 7×24 with automatic reconnection, health monitoring, and low memory footprint.

---

## Features

- **YouTube live → Bilibili relay** — pulls HLS with `yt-dlp`, pushes RTMP with FFmpeg.
- **Auto-reconnect with exponential backoff** — configurable retry budget, intervals, and backoff multiplier.
- **Smart transcode** — `copy` / `auto` / `force`; in `auto` mode each stream is decided independently, so H.264 video and AAC audio pass through untouched and only incompatible codecs (VP9, AV1, …) are re-encoded.
- **HLS self-healing pull** — FFmpeg `reconnect` flags plus a 10s read timeout so a hung segment fetch can't wedge the pipeline.
- **Low memory** — measured ≈30–44 MiB in copy mode, ≈256 MiB when transcoding with `threads: 2`.
- **`/healthz` status endpoint** — per-pipeline JSON: state, uptime, last error, bitrate, fps, FFmpeg stderr tail.
- **Stream-URL cache** — resolved HLS URLs are reused for 10 minutes, so transient failures reconnect instantly without a full `yt-dlp` re-extraction.
- **Multi-pipeline** — run several relays in one process, independently.
- **Docker multi-stage image** — minimal Alpine runtime with static FFmpeg and a current `yt-dlp` bundled.

## Project structure

```
restream/
├── main.go                 # entry point, CLI flags, /healthz HTTP server
├── config/
│   └── config.go           # YAML loading, ${ENV} expansion, CLI overrides
├── source/
│   ├── source.go           # Source interface + StreamInfo
│   ├── register.go         # source registry (name → factory)
│   └── youtube/youtube.go  # YouTube live source (shells out to yt-dlp)
├── sink/
│   ├── sink.go             # Sink interface + RTMPTarget
│   ├── register.go         # sink registry (name → factory)
│   └── bilibili/bilibili.go# Bilibili RTMP sink
├── ffmpeg/
│   └── pipeline.go         # FFmpeg command build (HLS pull, transcode, RTMP push)
├── pipeline/
│   └── manager.go          # per-pipeline lifecycle: resolve → run → retry/backoff
└── health/
    ├── checker.go          # FFmpeg stderr monitoring (stall / error detection)
    └── registry.go         # per-pipeline status, served by /healthz
```
## Prerequisites

- **Go 1.22+** — only needed to build from source.
- **FFmpeg** — runtime binary, must be in `PATH`.
- **yt-dlp (latest)** — install via `pipx install yt-dlp` or `pip install -U yt-dlp`. **Distro packages (apt/brew/pacman) are too old** for the PO-token / n-sig challenges YouTube requires in 2026.
- **PO Token Provider sidecar — REQUIRED** for YouTube in 2026. restream's yt-dlp plugin auto-connects to `127.0.0.1:4416`; without a provider listening there, stream resolution fails. This applies to CLI and config-file modes too, not just Docker:

```bash
docker run -d --name pot-provider --network host \
  --init --env TOKEN_TTL=6 --restart unless-stopped \
  brainicism/bgutil-ytdlp-pot-provider:latest
# verify:
curl -fsS http://127.0.0.1:4416/ping
```
## Quick start

### 1. CLI mode (single pipeline, no config file)

```bash
go build -o restream .
./restream --url "https://www.youtube.com/watch?v=LIVE_ID" \
           --key "YOUR_BILIBILI_STREAM_KEY" \
           --transcode auto
```

Other flags: `--config <path>`, `--log-level debug|info|warn|error`, `--version`.

### 2. Config file mode

```bash
cp config.yaml.example config.yaml   # edit, then:
./restream --config config.yaml
```
### 3. Docker Compose (recommended)

```bash
cp config.yaml.example config.yaml        # edit your config
export BILIBILI_STREAM_KEY="YOUR_KEY"     # or hardcode in config.yaml
docker compose up -d
docker compose logs -f
```

> The compose file runs **both** services with `network_mode: host` — the `pot-provider` and the `restream` container share the host's network stack so yt-dlp reaches the PO-token provider at `127.0.0.1:4416`. On the default bridge network `127.0.0.1` would be the container itself and PO-token auth would silently fail.

## Configuration

`config.yaml.example` is fully commented. All `${VAR}` values are expanded from the environment at load time.

| Section / key | Default | Description |
|---|---|---|
| `global.log_level` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `global.health_check_interval` | `10` | Stall-detection timeout (seconds) — if FFmpeg reports no progress this long, the stream is considered stalled and reconnected. **This is a stall timeout, not a poll interval** (clamped to ≥ 3). |
| `global.http_addr` | `:8080` | `GET /healthz` listen address (JSON); empty disables it. |
| `pipeline.name` | — | Pipeline identifier, used in logs and `/healthz`. |
| `source.type` | — | Registered source name, e.g. `youtube`. |
| `source.config.url` | — | Live-stream URL (**required**). |
| `source.config.format` | `best` | yt-dlp format selector. Use `best` for live (see note below). |
| `source.config.proxy` | `""` | HTTP/SOCKS proxy, e.g. `socks5://127.0.0.1:1080`. |
| `source.config.force_ipv4` | `false` | Force IPv4 when your proxy only supports IPv4. |
| `sink.type` | — | Registered sink name, e.g. `bilibili`. |
| `sink.config.rtmp_url` | `rtmp://live-push.bilivideo.com/live-bvc/` | RTMP ingest URL, up to `live-bvc/` (see key split below). |
| `sink.config.stream_key` | — | Stream key / code (**required**), supports `${BILIBILI_STREAM_KEY}`. |
| `ffmpeg.transcode` | `auto` | `auto` \| `copy` \| `force` (see table below). |
| `ffmpeg.video_encoder` | `libx264` | Video encoder (used when transcoding). |
| `ffmpeg.preset` | `veryfast` | x264 preset. |
| `ffmpeg.crf` | `23` | CRF 0–51, lower = better quality. |
| `ffmpeg.scale` | `""` | Resolution scaling, e.g. `-1:720` (transcode only). |
| `ffmpeg.audio_encoder` | `aac` | Audio encoder. |
| `ffmpeg.audio_bitrate` | `128k` | Audio bitrate. |
| `ffmpeg.threads` | `0` | Encoder threads; `0` = FFmpeg default (one per core, highest memory). Limit to cap memory (e.g. `2` ≈ 256 MiB). |
| `ffmpeg.maxrate` | `""` | Uplink video bitrate cap for weak connections, e.g. `6M` (adds `-maxrate 6M -bufsize 6M`); transcode only, ignored in `copy` mode. |
| `retry.max_retries` | `0` | `0` = retry forever. |
| `retry.initial_interval` | `5` | First retry delay (seconds). |
| `retry.max_interval` | `60` | Backoff cap (seconds). |
| `retry.backoff_multiplier` | `2.0` | Exponential factor per retry. |

### Transcode modes

| Mode | Behavior | CPU |
|---|---|---|
| `copy` | Stream-copy everything, no re-encoding | ≈ zero |
| `auto` | Re-encode only codecs that aren't FLV-compatible; H.264/AAC pass through | moderate |
| `force` | Always re-encode video + audio with the configured params | highest |

> **On `format` for live streams:** the default `best` (single merged HLS stream) is the reliable choice. `bestvideo+bestaudio` is **VOD-oriented** — live streams usually have no separate audio/video tracks, so yt-dlp errors out. Set it explicitly only if you truly need separate tracks.

### Bilibili stream-key split (most common first-run mistake)

Bilibili's live dashboard gives you one full RTMP URL:

```
rtmp://live-push.bilivideo.com/live-bvc/?streamname=abc_123&key=xxx&pflag=2
```

restream splits it into two fields:

- `rtmp_url` → everything **up to** `live-bvc/`: `rtmp://live-push.bilivideo.com/live-bvc/`
- `stream_key` → everything **after** `?`: `streamname=abc_123&key=xxx&pflag=2`

```yaml
sink:
  type: bilibili
  config:
    rtmp_url: "rtmp://live-push.bilivideo.com/live-bvc/"
    stream_key: "streamname=abc_123&key=xxx&pflag=2"
```

Putting the whole address into `stream_key`, or including `?streamname=...` in `rtmp_url`, breaks the RTMP handshake.

## Health endpoint

`global.http_addr` (default `:8080`) serves `GET /healthz` with per-pipeline JSON status — handy for 7×24 monitoring / alerting:

```bash
curl -s http://127.0.0.1:8080/healthz
# {"youtube-to-bilibili":{"state":"running","started":"...","uptime":3600,"bitrate":"4561.0kbits/s","fps":30,"stderr_tail":[]}}
```

- `state`: `resolving` / `running` / `backoff` / `stopped`
- `last_error`: most recent failure reason; `stderr_tail`: last 20 lines of FFmpeg stderr (diagnostics)
- With Docker, the compose healthcheck probes `http://127.0.0.1:8080/healthz` (host network).

## Environment variables

| Variable | Purpose |
|---|---|
| `BILIBILI_STREAM_KEY` | Bilibili stream key; referenced in config as `${BILIBILI_STREAM_KEY}` |

Any config value may reference an environment variable with `${VAR}` syntax; it is expanded at startup. An unresolved `${VAR}` is an error at load time.

## Extending

restream is platform-agnostic behind two interfaces and a registry. A new platform = implement an interface, register it in `init()`, and blank-import it in `main.go`.

**Add a Source** (e.g. `source/twitch/twitch.go`):

```go
type Source interface {
    Name() string
    GetStream(ctx context.Context, url string) (*source.StreamInfo, error)
    ValidateURL(url string) error
}
func init() { source.Register("twitch", New) }
```

Then in `main.go`: `import _ "github.com/wings1848/restream/source/twitch"`.

**Add a Sink** (e.g. `sink/huya/huya.go`):

```go
type Sink interface {
    Name() string
    GetTarget(ctx context.Context, config map[string]string) (*sink.RTMPTarget, error)
    ValidateConfig(config map[string]string) error
}
func init() { sink.Register("huya", New) }
```

Then in `main.go`: `import _ "github.com/wings1848/restream/sink/huya"`.

## Troubleshooting

**`yt-dlp failed: ...`**
- `yt-dlp` must be current (`pipx install -U yt-dlp`) and in `PATH`.
- Confirm the stream is genuinely live (not scheduled / ended).
- **Check the PO Token Provider** is reachable — CLI and config modes need it too: `curl -fsS http://127.0.0.1:4416/ping`.

**`ffmpeg exited with error`**
- Confirm FFmpeg ≥ 4.0 is in `PATH` and the Bilibili key is valid.
- Try `ffmpeg.transcode: copy` to rule out encoder issues.
- The FFmpeg stderr tail is now surfaced in `GET /healthz` (`stderr_tail`) and in debug logs — start with `--log-level debug`.

**Frequent disconnects**
- Check network stability / proxy.
- Increase `retry.initial_interval` and `retry.max_interval`.
- For a weak uplink, cap the bitrate with `ffmpeg.maxrate` (e.g. `6M`) — note it only applies when transcoding.

**Bilibili handshake fails**
- Re-check the key split: `rtmp_url` up to `live-bvc/`, `stream_key` = the `?streamname=...` part (see above).

## License

MIT
