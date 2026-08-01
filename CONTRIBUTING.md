# Contributing to restream

Thanks for your interest in contributing to restream! This project is a
YouTube-to-Bilibili live stream relay written in Go. We welcome bug reports,
feature requests, and pull requests.

## Reporting issues

Before opening an issue, please check the existing issues to avoid duplicates.
When reporting a bug, include as much of the following as possible:

- **restream version**: run `restream --version` and paste the output.
- **Reproduction steps**: the exact command or config that triggers the bug.
- **Logs**: capture the restream stderr logs (slog text output) at `--log-level debug`.
- **Health status**: if `/healthz` is enabled, paste its JSON output
  (`curl http://localhost:8080/healthz`). It shows each pipeline's state,
  last error, and recent FFmpeg stderr tail.
- **Sanitized config**: include your `config.yaml`, but **redact** your
  Bilibili `stream_key`, cookies, and any other secrets. Never post live
  credentials.

A good issue helps us reproduce the problem quickly. If you can, also note
your OS, Go version (`go version`), FFmpeg version (`ffmpeg -version`), and
yt-dlp version (`yt-dlp --version`).

## Development setup

Requirements:

- Go 1.22 or newer
- FFmpeg (with libx264 and FLV support)
- yt-dlp
- `gopkg.in/yaml.v3` (fetched automatically by the Go toolchain)

Clone the repository and build:

```sh
git clone https://github.com/wings1848/restream
cd restream
go build ./...
```

Run the test suite and static checks:

```sh
go test ./...
go vet ./...
gofmt -l .   # should print nothing; run `gofmt -w .` if it lists files
```

The full test suite exercises config parsing/validation, FFmpeg command
building, the health checker, and the YouTube source.

## Code style

- **Formatting**: code must be formatted with `gofmt`. Run
  `gofmt -l .` and fix anything it reports before submitting.
- **Error handling**: wrap errors with `%w` so call sites can use
  `errors.Is` / `errors.As`:

  ```go
  return nil, fmt.Errorf("reading config file %s: %w", path, err)
  ```

  Avoid `fmt.Errorf` without `%w`, and avoid swallowing errors with `_`.
- **Comments**: exported identifiers must have a doc comment starting with the
  identifier name. Explain *why* where the code doesn't make it obvious
  (e.g. FFmpeg flag choices, non-obvious regexes).
- **Naming**: follow idiomatic Go naming. Keep functions small and focused.

## Pull request process

1. Fork the repository and create a feature branch
   (`git checkout -b feat/my-change`).
2. Make your change with small, focused commits (see commit style below).
3. Add or update tests for any new or changed behavior. New features should
   come with tests; bug fixes should add a regression test.
4. Run the full checks locally: `gofmt -l .`, `go vet ./...`,
   `go build ./...`, and `go test ./...`.
5. Open a pull request against `master`. In the description, summarize what
   changed and why, and note any manual verification you performed.

Keep pull requests small and focused on a single concern. If a change is large,
split it into several PRs. A reviewer should be able to understand your change
at a glance.

## Commit message style

restream uses [Conventional Commits](https://www.conventionalcommits.org/).
Each commit message should start with a type and, when relevant, a scoped
subject:

- `feat:` a new feature or user-visible capability
- `fix:` a bug fix
- `docs:` documentation-only changes
- `refactor:` code changes that neither fix a bug nor add a feature
- `test:` adding or updating tests
- `chore:` maintenance tasks (dependencies, CI, tooling, secrets hygiene)

Examples:

```text
feat(youtube): add force_ipv4 option to YouTube source
fix(health): split FFmpeg progress on CRLF line endings
test: add config validation tests for retry fields
chore: ignore config.yaml, .env and cookies.txt (secrets)
```

Use the imperative mood in the subject line, keep it under ~72 characters,
and explain the motivation in the body when it isn't obvious from the subject.
