# Plan: animesucc-cli

## Context

Build a Go CLI app that searches animesaturn.cx, lets the user pick an anime from results, then downloads all (or selected) episodes as mp4 files. The site serves video via both direct `.mp4` URLs and `.m3u8` HLS playlists. m3u8 downloads prefer shelling out to `ffmpeg` but fall back to native Go HLS segment download if ffmpeg is not installed. Downloads run in parallel with per-episode progress bars.

Architecture is designed so the core logic can be driven by any frontend (CLI, HTTP server, etc.) — the CLI is just one adapter.

## Website Flow (verified by scraping)

1. **Search API**: `GET /index.php?search=1&key=<query>` returns JSON array:
   ```json
   [{"name":"Naruto","link":"Naruto-aaaaaaa","image":"...","release":"...","state":"1"}, ...]
   ```
2. **Anime page**: `GET /anime/<link>` — scrape all `href="https://.../ep/<slug>"` anchors (all episodes on one page, grouped in ranges)
3. **Episode page**: `GET /ep/<slug>` — scrape `href="...watch?file=<hash>"` link
4. **Watch page**: `GET /watch?file=<hash>` — extract video URL from:
   - JWPlayer: `jwplayer('player_hls').setup({ file: "URL" })` (regex: `file:\s*"([^"]+)"`)
   - `<source>` element: `<source src="URL" type="video/mp4" />` or `type="application/x-mpegURL"`
   - If primary fails, try alt player: `GET /watch?file=<hash>&s=alt`
5. **Download**: `.mp4` = direct HTTP GET; `.m3u8` = ffmpeg or native HLS

## CLI Interface

```
animesucc <search> [flags]

Flags:
  -e, --episodes string   Episode filter (e.g. "1-5,8,10-12"). Default: all
  -p, --position int      Pick Nth search result (1-based). Default: interactive prompt
  -o, --output string     Output directory. Default: current dir
  -j, --parallel int      Max concurrent downloads. Default: 3
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework (industry standard for Go CLIs) — CLI adapter only |
| `github.com/PuerkitoBio/goquery` | HTML scraping with CSS selectors (jQuery-like, 14k stars) |
| `github.com/grafov/m3u8` | M3U8/HLS playlist parsing (most established Go m3u8 lib, 1.2k stars) |
| `github.com/yapingcat/gomedia` | Pure Go TS→MP4 remuxing for native HLS fallback (500+ stars) |
| `github.com/vbauerster/mpb/v8` | Multiple concurrent progress bars — CLI adapter only |

## Architecture

```
                    ┌──────────────┐
                    │   main.go    │
                    └──────┬───────┘
                           │
              ┌────────────┴────────────┐
              │                         │
     ┌────────▼────────┐     ┌─────────▼─────────┐
     │  cmd/ (CLI)      │     │  (future: HTTP)    │
     │  cobra + mpb     │     │  gin/chi + SSE     │
     │  stdin prompts   │     │  REST endpoints    │
     └────────┬─────────┘     └─────────┬──────────┘
              │   implements            │  implements
              └────────────┬────────────┘
                           │
              ┌────────────▼────────────┐
              │   animesucc/ (core)     │
              │                         │
              │  types.go  — interfaces │
              │  search.go              │
              │  scraper.go             │
              │  download.go            │
              │  hls.go                 │
              │  episodes.go            │
              └─────────────────────────┘
```

### Key Interfaces

The core library uses two key interfaces that frontends implement:

#### `ProgressReporter` — progress/status callbacks

```go
type ProgressReporter interface {
    OnEpisodeStart(ep Episode, index, total int)
    OnProgress(ep Episode, bytesDownloaded, totalBytes int64)
    OnEpisodeComplete(ep Episode, err error)
    OnAllComplete(succeeded, failed int)
}
```

- **CLI**: `mpb` progress bars
- **HTTP** (future): SSE events or websocket messages

#### `OutputFactory` — where downloads are written

```go
// OutputFactory creates a write destination for a given episode.
// Returns: writer (must be seekable for MP4 muxing), cleanup func, error.
type OutputFactory func(ep Episode) (io.WriteSeeker, func() error, error)
```

- **CLI**: opens a file in the output directory, cleanup = close the file
- **HTTP** (future): creates a temp file, cleanup = no-op (file stays for serving via HTTP endpoint); or writes to a managed storage path that the HTTP server knows about

Note: `io.WriteSeeker` (not just `io.Writer`) because MP4 container muxing requires seeking to write the `moov` atom. This means true HTTP streaming of MP4 isn't possible, but the file can be served after download completes.

### Core API (no UI concerns)

```go
// animesucc/client.go
type Client struct { httpClient *http.Client; baseURL string }

// animesucc/search.go
func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error)

// animesucc/scraper.go
func (c *Client) GetEpisodes(ctx context.Context, animeLink string) ([]Episode, error)
func (c *Client) GetVideoURL(ctx context.Context, episodeURL string) (*VideoSource, error)

// animesucc/download.go — output destination is injected, not hardcoded
func (c *Client) DownloadAll(ctx context.Context, episodes []Episode, opts DownloadOptions, output OutputFactory, progress ProgressReporter) error

// animesucc/episodes.go
func ParseEpisodeFilter(s string) (EpisodeFilter, error)
func (f EpisodeFilter) Apply(episodes []Episode) []Episode
```

User selection (picking from search results) stays in the CLI adapter — it's inherently a UI concern. The core just returns the results list.

## File Structure

```
.
├── main.go                # Entry point: creates core client, runs CLI
├── go.mod / go.sum
├── animesucc/             # Core library (no CLI/HTTP dependency)
│   ├── types.go           # SearchResult, Episode, VideoSource, ProgressReporter interface, DownloadOptions
│   ├── client.go          # HTTP client with shared config (User-Agent, timeouts)
│   ├── search.go          # Search(ctx, query) → []SearchResult
│   ├── scraper.go         # GetEpisodes(ctx, link), GetVideoURL(ctx, epURL)
│   ├── download.go        # DownloadAll(ctx, episodes, opts, progress)
│   ├── hls.go             # HLS download: ffmpeg path + native Go path
│   └── episodes.go        # ParseEpisodeFilter, EpisodeFilter.Apply
├── cmd/
│   └── root.go            # cobra command, flag parsing, interactive selection, mpb progress bars
```

## Implementation Approach: Test-First

Every component is implemented test-first: write tests, then write the code to make them pass. Tests against animesaturn assume the site is reachable (real HTTP calls, no mocks). For file content comparison, use SHA-256 digests or byte-by-byte comparison. Independent tests call `t.Parallel()` to run concurrently.

## Implementation Steps

### 1. Project scaffolding
- `go mod init github.com/mrschyzo/animesucc-cli`
- Add dependencies
- `main.go` as thin entry point

### 2. `animesucc/types.go` — Core types and interfaces
- `SearchResult{Name, Link, Image, Release, State}`
- `Episode{Number int, Slug, URL, WatchURL string}`
- `VideoSource{URL, Type string}` (Type: "mp4" or "m3u8")
- `DownloadOptions{MaxParallel int}` (no OutputDir — that's the CLI adapter's concern)
- `ProgressReporter` interface
- `OutputFactory` function type
- `EpisodeFilter` type

### 3. `animesucc/episodes.go` — Episode range parser
- **Tests first** (`episodes_test.go`): parse "1-5,8,10-12", single number, empty string, invalid input, Apply on episode list
- Parse `"1-5,8,10-12"` into an `EpisodeFilter`
- `Apply(episodes)` filters the episode list
- Empty string = pass-through (all episodes)

### 4. `animesucc/client.go` — Shared HTTP client
- `NewClient()` with sensible defaults (User-Agent, timeout, redirect policy)

### 5. `animesucc/search.go` — Search API
- **Tests first** (`search_test.go`): search "naruto" against live API, verify results contain expected names and non-empty links
- `Search(ctx, query) → []SearchResult` — GET JSON API, unmarshal

### 6. `animesucc/scraper.go` — HTML scraping
- **Tests first** (`scraper_test.go`):
  - `GetEpisodes`: scrape Naruto page, verify 220 episodes, verify episode numbers are sequential
  - `GetVideoURL`: scrape Frieren ep 1, verify returns mp4 URL; scrape Naruto ep 1, verify returns m3u8 URL
- `GetEpisodes(ctx, link)` — fetch anime page, extract `/ep/` hrefs, deduplicate, parse episode numbers
- `GetVideoURL(ctx, epURL)`:
  1. Fetch episode page → extract `watch?file=` link
  2. Fetch watch page → extract video URL via regex + goquery
  3. If not found, retry with `&s=alt`

### 7. `animesucc/download.go` — Download orchestrator
- **Tests first** (`download_test.go`):
  - Download a single known mp4 episode to temp dir, verify file exists and has non-zero size
  - Verify SHA-256 digest is stable across runs for same episode
  - Verify progress callbacks are invoked
- `DownloadAll(ctx, episodes, opts, output, progress)` — semaphore-gated goroutines
- For each episode: call `output(ep)` to get writer, resolve video URL, dispatch to mp4 or HLS download, call cleanup
- `downloadMP4(ctx, url, writer, progressFn)` — HTTP GET, write to `io.WriteSeeker`, track progress via Content-Length

### 8. `animesucc/hls.go` — HLS download
- **Tests first** (`hls_test.go`):
  - Download a known m3u8 episode (Naruto ep 1) with ffmpeg, verify output is valid mp4 (non-zero, starts with ftyp or moov atom)
  - Download same episode with native Go fallback, verify output is valid mp4
  - Compare SHA-256 digests of both outputs (may differ due to muxing differences — so just verify both are valid mp4)
- `downloadHLS(ctx, url, writer, progressFn)`:
  1. `exec.LookPath("ffmpeg")` → if found: shell out
  2. Native fallback: `grafov/m3u8` parse → download segments → `gomedia` TS→MP4 remux

### 9. `cmd/root.go` — CLI adapter
- cobra command with flags (`-e`, `-p`, `-o`, `-j`)
- Interactive search result selection (numbered list + stdin prompt)
- `CLIProgressReporter` implementing `ProgressReporter` with `mpb/v8` progress bars
- `FileOutputFactory(dir string) OutputFactory` — creates files like `<dir>/<AnimeName>_Ep<N>.mp4`, cleanup = close file

### 10. `main.go` — Wire CLI
- Create `animesucc.Client`, pass to cobra command

## Verification

1. `go test ./animesucc/...` — all tests pass
2. `go build` — compiles
3. `./animesucc-cli "frieren" -e 1 -p 1 -o /tmp/test` — downloads single episode
4. Test with m3u8 source (Naruto) and mp4 source (Frieren)
5. Test ffmpeg fallback by temporarily renaming ffmpeg
6. Test episode range parsing: `"1-3,5"`, `""`, `"1"`, `"1-1000"` (clamps to actual count)
