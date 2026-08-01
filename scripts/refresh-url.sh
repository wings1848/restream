#!/bin/bash
# refresh-url.sh — resolve a YouTube live stream to its signed HLS URL on a
# trusted machine and push it to a restream server whose datacenter IP cannot
# resolve itself (YouTube bot check) but can fetch signed manifest URLs.
#
# Signed URLs live ~6h; run from cron/systemd every ~3h:
#   0 */3 * * * /path/to/scripts/refresh-url.sh
#
# Env:
#   YT_URL      YouTube live URL (required)
#   SSH_HOST    ssh alias/host to push to (default: butterfly-jp)
#   REMOTE_FILE target file on the server (default: /home/azureuser/restream/stream.url)
#   PROXY       HTTP proxy for yt-dlp (default: http://127.0.0.1:7897)
#   IMAGE       docker image containing yt-dlp (default: restream:clean)
set -euo pipefail

YT_URL="${YT_URL:?set YT_URL to the YouTube live URL}"
SSH_HOST="${SSH_HOST:-butterfly-jp}"
REMOTE_FILE="${REMOTE_FILE:-/home/azureuser/restream/stream.url}"
PROXY="${PROXY:-http://127.0.0.1:7897}"
IMAGE="${IMAGE:-restream:clean}"

echo "== resolving $YT_URL (via $PROXY)"
URL="$(docker run --rm --network host --entrypoint yt-dlp "$IMAGE" \
  --proxy "$PROXY" -g "$YT_URL" 2>/dev/null | tail -1)"
if [ -z "$URL" ]; then
  echo "!! resolve failed (bot check? network?)" >&2
  exit 1
fi

# Atomic replace on the server: write a temp file, then mv.
echo "== pushing to ${SSH_HOST}:${REMOTE_FILE}"
printf '%s\n' "$URL" | ssh "$SSH_HOST" \
  "install -m 0644 /dev/stdin '$REMOTE_FILE.tmp' && mv -f '$REMOTE_FILE.tmp' '$REMOTE_FILE'"

exp="$(printf '%s' "$URL" | sed -n 's|.*expire/\([0-9][0-9]*\).*|\1|p')"
if [ -n "$exp" ]; then
  echo "== ok, URL valid until: $(date -d "@$exp" '+%F %T')"
else
  echo "== ok (no expire param found in URL)"
fi
