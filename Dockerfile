# =============================================================================
# Stage 1: Build the restream binary
# =============================================================================
FROM golang:1.22-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

RUN apk add --no-cache ca-certificates git

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o restream .

# =============================================================================
# Stage 2: Download static ffmpeg (~15MB, no runtime library dependencies)
# https://johnvansickle.com/ffmpeg/ — musl static build with x264/aac/flv/rtmp
# =============================================================================
FROM alpine:3.22 AS ffmpeg-dl

ARG FFMPEG_ARCH=amd64

RUN apk add --no-cache curl xz \
    && curl -fsSL -o /tmp/ffmpeg.tar.xz \
       "https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-${FFMPEG_ARCH}-static.tar.xz" \
    && tar -xJf /tmp/ffmpeg.tar.xz -C /tmp \
    && mv /tmp/ffmpeg-*/ffmpeg /usr/local/bin/

# =============================================================================
# Stage 3: Minimal runtime image
# =============================================================================
FROM alpine:3.22

RUN apk add --no-cache \
    python3 \
    py3-pip \
    nodejs \
    ca-certificates \
    tzdata \
    && pip3 install --break-system-packages --no-cache-dir -U \
        "yt-dlp[default]" bgutil-ytdlp-pot-provider \
    && rm -rf /usr/lib/python3.12/test /usr/lib/python3.12/idlelib \
              /usr/lib/python3.12/turtledemo \
    && find /usr/lib/python3.12 -type d -name __pycache__ -prune -exec rm -rf {} + \
    && echo '--js-runtimes node' > /etc/yt-dlp.conf

# Node.js is yt-dlp's JS runtime for YouTube n-sig challenge solving, enabled
# via /etc/yt-dlp.conf (deno is yt-dlp's default but is ~90MB; node is the
# smaller supported option).
# "yt-dlp[default]" is REQUIRED: a plain `pip install yt-dlp` omits the EJS
# challenge-solver scripts (yt-dlp-ejs), so n-challenge solving fails and
# resolve returns "No video formats found" even with a valid JS runtime.

COPY --from=builder /app/restream /usr/local/bin/restream
COPY --from=ffmpeg-dl /usr/local/bin/ffmpeg /usr/local/bin/

ENTRYPOINT ["restream"]
CMD ["--help"]
