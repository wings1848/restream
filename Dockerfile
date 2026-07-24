# =============================================================================
# Stage 1: Build the restream binary
# =============================================================================
FROM golang:1.22-alpine AS builder

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
    yt-dlp \
    ffmpeg \
    ca-certificates \
    tzdata

COPY --from=builder /app/restream /usr/local/bin/restream

ENTRYPOINT ["restream"]
CMD ["--help"]
