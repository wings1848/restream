# Deployment

There are three ways to run restream: as a plain CLI command, from a config file, or with Docker Compose (recommended). All three modes depend on the same runtime prerequisites.

## Prerequisites

### Building from source

- **Go 1.22+** — only needed to build the binary from source.
- **FFmpeg** — runtime binary, must be in `PATH` (version ≥ 4.0).

### yt-dlp (must be current)

Install the **latest** yt-dlp, never the distro package:

```bash
pipx install yt-dlp        # or: pip install -U yt-dlp
```

**Distro packages (apt/brew/pacman) are too old** to handle the PO-token / n-sig challenges YouTube requires in 2026.

### PO Token Provider sidecar — REQUIRED

YouTube in 2026 requires PO-token authentication. restream's yt-dlp plugin auto-connects to `127.0.0.1:4416`; **without a provider listening there, stream resolution fails**. This applies to CLI and config-file modes too, not just Docker:

```bash
docker run -d --name pot-provider --network host \
  --init --env TOKEN_TTL=6 --restart unless-stopped \
  brainicism/bgutil-ytdlp-pot-provider:latest
# verify:
curl -fsS http://127.0.0.1:4416/ping
```

The provider must listen on the same network namespace as yt-dlp (i.e. the same host when running restream natively). `--network host` binds it to the host's `127.0.0.1:4416`.

## Option 1: CLI mode (single pipeline, no config file)

```bash
go build -o restream .
./restream --url "https://www.youtube.com/watch?v=LIVE_ID" \
           --key "YOUR_BILIBILI_STREAM_KEY" \
           --transcode auto
```

Available flags:

| Flag | Purpose |
|---|---|
| `--url` | YouTube live URL (quick-start, no config file) |
| `--key` | Bilibili stream key (quick-start, no config file) |
| `--transcode` | `auto` \| `copy` \| `force` |
| `--config` | Path to `config.yaml` |
| `--log-level` | `debug` \| `info` \| `warn` \| `error` (defaults to config `global.log_level`, then `info`) |
| `--version` | Print version and exit |

## Option 2: Config file mode

```bash
cp config.yaml.example config.yaml   # edit it
./restream --config config.yaml
```

See [docs/configuration.md](configuration.md) for the full field reference.

## Option 3: Docker Compose (recommended)

```bash
cp config.yaml.example config.yaml        # edit your config
export BILIBILI_STREAM_KEY="YOUR_KEY"     # or hardcode it in config.yaml
docker compose up -d
docker compose logs -f
```

The compose file runs **both** services with `network_mode: host`:

- The `pot-provider` and the `restream` container share the host's network stack, so yt-dlp reaches the PO-token provider at `127.0.0.1:4416`.
- On the default bridge network `127.0.0.1` would be the container itself and PO-token auth would silently fail.
- `restream` also depends on `pot-provider` being healthy before it starts.
- The restream container mounts `./config.yaml` read-only at `/etc/restream/config.yaml` and runs with `--config /etc/restream/config.yaml`.
- Optional resource ceilings for the transcode path are commented out in the compose file (`mem_limit`, `cpus`) — uncomment to apply.

### Environment variables

| Variable | Purpose |
|---|---|
| `BILIBILI_STREAM_KEY` | Bilibili stream key, referenced in the config as `${BILIBILI_STREAM_KEY}` |

The compose file passes `BILIBILI_STREAM_KEY=${BILIBILI_STREAM_KEY}` through from the shell. Any config value can reference `${VAR}`; an unresolved `${VAR}` is a load-time error.

## Building the binary

```bash
go build -o restream .
```

## Building the Docker image

The `Dockerfile` is a multi-stage build:

1. `golang:1.22-alpine` builds the static restream binary.
2. A second stage downloads the **static** FFmpeg from John Van Sickle's musl builds (bundles x264/aac/flv/rtmp, no runtime library dependencies).
3. The runtime image is a minimal `alpine:3.22` with Python, pip-installed **latest** yt-dlp + `bgutil-ytdlp-pot-provider`, and QuickJS as yt-dlp's JS runtime for n-sig solving (`--js-runtimes quickjs` in `/etc/yt-dlp.conf`).

```bash
docker build -t restream .
```

The compose file builds it automatically via the `build:` section. For a different target architecture you can override the `FFMPEG_ARCH` build arg (default `amd64`).
