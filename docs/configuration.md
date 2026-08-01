# Configuration Reference

restream is configured with a YAML file (`config.yaml` — copy from `config.yaml.example`). The CLI flags are only a quick-start shortcut; for anything non-trivial use a config file. Every config value can reference an environment variable with `${VAR}` syntax, which is expanded at startup.

## Complete YAML reference

```yaml
global:
  log_level: info              # debug | info | warn | error
  health_check_interval: 10    # stall timeout (seconds), see note below
  http_addr: ":8080"           # /healthz listen address; "" disables it

pipelines:
  - name: "youtube-to-bilibili"
    source:
      type: youtube            # registered source name
      config:
        url: "https://www.youtube.com/watch?v=YOUR_VIDEO_ID"   # required
        format: ""             # yt-dlp format selector; default "best" for live
        proxy: ""              # HTTP/SOCKS proxy, e.g. socks5://127.0.0.1:1080
        force_ipv4: "false"    # force IPv4 if your proxy only supports IPv4
    sink:
      type: bilibili           # registered sink name
      config:
        rtmp_url: "rtmp://live-push.bilivideo.com/live-bvc/"
        stream_key: "${BILIBILI_STREAM_KEY}"   # required
    ffmpeg:
      transcode: auto          # auto | copy | force
      video_encoder: libx264
      preset: veryfast
      crf: 23
      scale: ""                # e.g. "-1:720" (transcode only)
      audio_encoder: aac
      audio_bitrate: 128k
      threads: 2               # default 2; 0 = FFmpeg default (one per core)
      maxrate: "8M"            # uplink cap (transcode only), default 8M
    retry:
      max_retries: 0           # 0 = retry forever
      initial_interval: 5      # first retry delay (seconds)
      max_interval: 60         # backoff cap (seconds)
      backoff_multiplier: 2.0  # exponential factor per retry
```

You can add more entries under `pipelines:` — each pipeline runs concurrently and independently.

## Field reference

| Section / key | Default | Description |
|---|---|---|
| `global.log_level` | `info` | `debug` \| `info` \| `warn` \| `error` |
| `global.health_check_interval` | `10` | **Stall-detection timeout** (seconds): if FFmpeg reports no progress for this long, the stream is considered stalled and reconnected. This is a stall timeout, **not** a poll interval (clamped to ≥ 3). Sane range 3–60; too low misfires on slow networks, too high delays recovery after a drop. |
| `global.http_addr` | `:8080` | `GET /healthz` listen address (JSON). Empty string disables the endpoint. |
| `pipeline.name` | — | Pipeline identifier, used in logs and `/healthz`. |
| `source.type` | — | Registered source name, e.g. `youtube`. |
| `source.config.url` | — | Live-stream URL (**required**). |
| `source.config.format` | `best` | yt-dlp format selector. Use `best` for live streams (see note below). Examples: `best`, `best[height<=1080]`. |
| `source.config.proxy` | `""` | HTTP/SOCKS proxy, e.g. `socks5://127.0.0.1:1080`. TUN mode (VPN) is preferred — no proxy config needed. |
| `source.config.force_ipv4` | `false` | Force IPv4 when your proxy only supports IPv4. |
| `sink.type` | — | Registered sink name, e.g. `bilibili`. |
| `sink.config.rtmp_url` | `rtmp://live-push.bilivideo.com/live-bvc/` | RTMP ingest URL, up to `live-bvc/` (see key split below). |
| `sink.config.stream_key` | — | Stream key / code (**required**). Supports `${BILIBILI_STREAM_KEY}`. |
| `ffmpeg.transcode` | `auto` | `auto` \| `copy` \| `force` (see table below). |
| `ffmpeg.video_encoder` | `libx264` | Video encoder (used when transcoding). |
| `ffmpeg.preset` | `veryfast` | x264 preset: `ultrafast` \| `superfast` \| `veryfast` \| `faster` \| `fast` \| `medium` \| `slow` \| `slower` \| `veryslow`. |
| `ffmpeg.crf` | `23` | Constant Rate Factor 0–51; lower = better quality, larger file. |
| `ffmpeg.scale` | `""` | Resolution scaling (transcode only). Examples: `-1:720` (720p, proportional width), `1280:720` (exact). Empty = no scaling. |
| `ffmpeg.audio_encoder` | `aac` | Audio encoder: `aac` \| `libmp3lame` \| `libopus` \| `copy`. |
| `ffmpeg.audio_bitrate` | `128k` | Audio bitrate, e.g. `64k` \| `128k` \| `192k` \| `256k` \| `320k`. |
| `ffmpeg.threads` | `2` | Encoder threads (≈ 256 MiB transcode memory for 1080p x264). Set `0` to keep FFmpeg's own default (one per CPU core, highest memory). |
| `ffmpeg.maxrate` | `"8M"` | Uplink video bitrate cap (adds `-maxrate 8M -bufsize 8M`), near Bilibili's 8000 kbps limit. Transcode only — ignored in `copy` mode, which forwards the source bitrate unchanged. |
| `retry.max_retries` | `0` | Maximum retry attempts; `0` = retry forever. |
| `retry.initial_interval` | `5` | First retry delay (seconds). |
| `retry.max_interval` | `60` | Backoff cap (seconds). |
| `retry.backoff_multiplier` | `2.0` | Exponential backoff factor applied per retry. |

## Transcode modes

| Mode | Behavior | CPU |
|---|---|---|
| `copy` | Stream-copy everything, no re-encoding. Source codec must be FLV-compatible (H.264 video + AAC audio). | ≈ zero |
| `auto` | Re-encode only codecs that aren't FLV-compatible; H.264/AAC pass through untouched. Each stream is decided independently. | moderate |
| `force` | Always re-encode video + audio with the configured params. | highest |

The FLV container only supports H.264 video and AAC audio natively. In `auto` mode, VP9, AV1, Opus, etc. are re-encoded to FLV-safe codecs; compatible streams are copied so you don't burn CPU re-encoding 24/7.

## Choosing the yt-dlp `format` for live streams

The default `best` (a single merged HLS stream) is the reliable choice for YouTube live.

`bestvideo+bestaudio` is **VOD-oriented** — live streams usually have no separate audio/video tracks, so yt-dlp errors out. Set it explicitly only if you truly need separate tracks.

## Bilibili stream-key split (most common first-run mistake)

Bilibili's live dashboard gives you one full RTMP URL:

```
rtmp://live-push.bilivideo.com/live-bvc/?streamname=abc_123&key=xxx&pflag=2
```

restream splits it into two config fields:

- `rtmp_url` → everything **up to** `live-bvc/`: `rtmp://live-push.bilivideo.com/live-bvc/`
- `stream_key` → everything **after** `?`: `streamname=abc_123&key=xxx&pflag=2`

```yaml
sink:
  type: bilibili
  config:
    rtmp_url: "rtmp://live-push.bilivideo.com/live-bvc/"
    stream_key: "streamname=abc_123&key=xxx&pflag=2"
```

Putting the whole address into `stream_key`, or including `?streamname=...` in `rtmp_url`, breaks the RTMP handshake (symptom: ffmpeg exits with "Connection refused" / exit status 251, or the Bilibili channel flickers on/off).

## Environment-variable expansion

Any config value may reference an environment variable with `${VAR}` syntax; it is expanded at load time. The one officially documented variable is:

| Variable | Purpose |
|---|---|
| `BILIBILI_STREAM_KEY` | Bilibili stream key; referenced in config as `${BILIBILI_STREAM_KEY}` |

> **An unresolved `${VAR}` is an error at load time** — restream refuses to start rather than silently pushing an empty key. This also applies to CLI mode.

## The `/healthz` endpoint config

`global.http_addr` (default `:8080`) serves `GET /healthz` with per-pipeline JSON status. Empty string disables it. See [docs/healthz.md](healthz.md) for the full field reference and monitoring tips.
