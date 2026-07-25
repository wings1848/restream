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
# Stage 2: Minimal runtime image
# =============================================================================
FROM alpine:3.20

RUN apk add --no-cache \
    python3 \
    py3-pip \
    nodejs \
    ffmpeg \
    ca-certificates \
    tzdata \
    && pip3 install --break-system-packages -U yt-dlp \
    && pip3 install --break-system-packages bgutil-ytdlp-pot-provider

# Enable Node.js as JS runtime for yt-dlp n-sig challenge solving.
# (Deno is preferred by default but not available on Alpine)
RUN mkdir -p /etc && echo '--js-runtimes node' > /etc/yt-dlp.conf

COPY --from=builder /app/restream /usr/local/bin/restream

ENTRYPOINT ["restream"]
CMD ["--help"]
