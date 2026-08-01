#!/usr/bin/env bash
# PostToolUse hook (restream project): gofmt -w any edited .go files.
# The edited file path comes from stdin JSON (tool_input.file_path) — the
# CLAUDE_FILE_PATHS_ABSOLUTE env var is NOT injected in this CC version.
input=$(cat)
file=$(python3 -c 'import json,sys
print(json.load(sys.stdin).get("tool_input", {}).get("file_path", ""))' <<<"$input")
[ -n "$file" ] || exit 0
case "$file" in
  *.go)
    command -v gofmt >/dev/null 2>&1 || exit 0
    gofmt -w "$file"
    ;;
esac
exit 0
