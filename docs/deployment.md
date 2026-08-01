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

There are two compose files:

- **`docker-compose.deploy.yml`** — runs the prebuilt **`ghcr.io/wings1848/restream:latest`** image. Use this for deployment; nothing is built locally, so no Go/FFmpeg build environment is needed.
- **`docker-compose.yml`** — builds the image from source via its `build:` section (development / self-builds).

### Deploying with the prebuilt image (recommended)

```bash
cp config.yaml.example config.yaml        # edit your config
export BILIBILI_STREAM_KEY="YOUR_KEY"     # or hardcode it in config.yaml
docker compose -f docker-compose.deploy.yml up -d
docker compose -f docker-compose.deploy.yml logs -f
```

### Building from source

```bash
cp config.yaml.example config.yaml        # edit your config
export BILIBILI_STREAM_KEY="YOUR_KEY"
docker compose up -d                      # uses docker-compose.yml (build:)
```

Both compose files run **both** services with `network_mode: host`:

- The `pot-provider` and the `restream` container share the host's network stack, so yt-dlp reaches the PO-token provider at `127.0.0.1:4416`.
- On the default bridge network `127.0.0.1` would be the container itself and PO-token auth would silently fail.
- `restream` also depends on `pot-provider` being healthy before it starts.
- The restream container mounts `./config.yaml` read-only at `/etc/restream/config.yaml` and runs with `--config /etc/restream/config.yaml`.
- Optional resource ceilings for the transcode path are commented out in the compose files (`mem_limit`, `cpus`) — uncomment to apply.

### Environment variables

| Variable | Purpose |
|---|---|
| `BILIBILI_STREAM_KEY` | Bilibili stream key, referenced in the config as `${BILIBILI_STREAM_KEY}` |

The compose file passes `BILIBILI_STREAM_KEY=${BILIBILI_STREAM_KEY}` through from the shell. Any config value can reference `${VAR}`; an unresolved `${VAR}` is a load-time error.

## Datacenter IPs and YouTube's bot check

YouTube bot-checks **resolution** (the player page) but not the **signed
manifest URLs** it produces. A machine on a residential IP can resolve a
signed HLS URL that any other IP — including a datacenter IP that would fail
its own resolution with "Sign in to confirm you're not a bot" — can then
fetch. PO tokens and fresh cookies do **not** fix a bot-checked datacenter
IP; the check is tied to the resolving IP's reputation.

If your server runs on a datacenter IP, split the roles with the `direct`
source:

1. On a trusted (residential) machine, resolve the stream periodically:
   `yt-dlp -g "<youtube url>"` (with your proxy/PO-token setup) → signed HLS URL.
2. Push that URL to a file on the server (e.g. via `scp`/`ssh` from a cron job).
3. Configure the server pipeline with `source.type: direct` and
   `source.config.url_file` pointing at that file; restream re-reads it on
   every resolve, so the URL is refreshed automatically on the next reconnect.

Signed URLs expire after ~6 hours; refresh the file at least every 3 hours.
Auto mode defaults unknown codecs to stream copy, which fits the typical
H.264/AAC HLS output — set `ffmpeg.transcode: force` if the source isn't
FLV-compatible.

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

The development `docker-compose.yml` builds it automatically via its `build:` section. For a different target architecture you can override the `FFMPEG_ARCH` build arg (default `amd64`).
