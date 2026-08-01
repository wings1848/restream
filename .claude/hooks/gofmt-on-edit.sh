#!/usr/bin/env bash
# PostToolUse hook (restream project): gofmt -w any edited .go files.
files=$(echo "${CLAUDE_FILE_PATHS_ABSOLUTE:-}" | tr ' ' '\n' | grep '\.go$')
if [ -z "$files" ]; then
  exit 0
fi
command -v gofmt >/dev/null 2>&1 || exit 0
for f in $files; do
  gofmt -w "$f"
done
exit 0
