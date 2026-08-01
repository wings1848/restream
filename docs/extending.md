# Extending restream

restream is platform-agnostic behind two interfaces and a registry. A new platform = implement an interface, register it in an `init()`, and blank-import the package in `main.go`. Use full module paths: the module is `github.com/wings1848/restream`.

## The `Source` interface (stream inputs)

```go
package source

type Source interface {
    // Name returns the platform identifier (e.g. "youtube", "douyin").
    Name() string
    // GetStream resolves the URL into a StreamInfo with access URLs and
    // codec metadata (VideoCodec, AudioCodec, Resolution, FPS, Bitrate).
    GetStream(ctx context.Context, url string) (*source.StreamInfo, error)
    // ValidateURL reports whether the URL is a valid live-stream URL.
    ValidateURL(url string) error
}

// Factory builds a Source from a flat string→string config map.
type Factory func(config map[string]string) (Source, error)
```

### Add a Source (e.g. `source/twitch/twitch.go`)

```go
package twitch

import (
    "context"
    "github.com/wings1848/restream/source"
)

type twitchSource struct{ /* ... */ }

func (s *twitchSource) Name() string { return "twitch" }

func (s *twitchSource) GetStream(ctx context.Context, url string) (*source.StreamInfo, error) {
    // resolve with yt-dlp or platform APIs; populate StreamInfo incl. codecs
    return &source.StreamInfo{
        URLs:       []string{"https://..."},
        VideoCodec: "h264",
        AudioCodec: "aac",
    }, nil
}

func (s *twitchSource) ValidateURL(url string) error { /* ... */ return nil }

func New(cfg map[string]string) (source.Source, error) { return &twitchSource{}, nil }

func init() {
    source.Register("twitch", New)
}
```

Then in `main.go`, add a blank import:

```go
import _ "github.com/wings1848/restream/source/twitch"
```

`source.Register` stores the factory by name; `source.New(name, cfg)` looks it up. Forgetting the blank import gives `source "twitch" is not registered (did you import its package?)`.

## The `Sink` interface (push destinations)

```go
package sink

type Sink interface {
    // Name returns the platform identifier (e.g. "bilibili", "huya").
    Name() string
    // GetTarget resolves configuration into a ready-to-use RTMP target.
    GetTarget(ctx context.Context, config map[string]string) (*sink.RTMPTarget, error)
    // ValidateConfig checks the config has all required keys / valid values.
    ValidateConfig(config map[string]string) error
}

// Factory builds a Sink from a flat string→string config map.
type Factory func(config map[string]string) (Sink, error)

type RTMPTarget struct {
    URL       string // e.g. "rtmp://live-push.bilivideo.com/live-bvc/"
    StreamKey string // e.g. "?streamname=...&key=..."
}
// FullURL() returns URL + StreamKey
```

### Add a Sink (e.g. `sink/huya/huya.go`)

```go
package huya

import (
    "context"
    "github.com/wings1848/restream/sink"
)

type huyaSink struct{ /* ... */ }

func (s *huyaSink) Name() string { return "huya" }

func (s *huyaSink) GetTarget(ctx context.Context, cfg map[string]string) (*sink.RTMPTarget, error) {
    return &sink.RTMPTarget{URL: cfg["rtmp_url"], StreamKey: cfg["stream_key"]}, nil
}

func (s *huyaSink) ValidateConfig(cfg map[string]string) error { /* ... */ return nil }

func New(cfg map[string]string) (sink.Sink, error) { return &huyaSink{}, nil }

func init() {
    sink.Register("huya", New)
}
```

Then in `main.go`:

```go
import _ "github.com/wings1848/restream/sink/huya"
```

## Wiring it together

1. The package's `init()` runs when it is imported (blank import in `main.go`).
2. `source.Register` / `sink.Register` adds the factory to the registry keyed by name.
3. Config `source.type` / `sink.type` selects the name; the flat `config:` map under each is passed to the factory.
4. FFmpeg pulls from `StreamInfo.URLs` and pushes to `RTMPTarget.FullURL()`.
