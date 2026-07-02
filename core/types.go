package animesucc

import (
	"context"
	"io"
)

type SearchResult struct {
	Name    string `json:"name"`
	Link    string `json:"link"`  // anime slug (no "/anime/" prefix); feeds GetEpisodes
	Image   string `json:"image"`
	Release string `json:"release"` // ".ac__sub" text, e.g. "15 Febbraio 2007 · 500 ep"
	State   string `json:"state"`   // no longer surfaced on the search card; kept for JSON compat
	Type    string `json:"type"`    // ".ac__type-badge" (TV / Movie / OVA …)
}

type Episode struct {
	Number int
	Slug   string
	URL    string
}

type VideoSourceType string

const (
	VideoSourceMP4     VideoSourceType = "mp4"
	VideoSourceM3U8    VideoSourceType = "m3u8"
	VideoSourceYouTube VideoSourceType = "youtube"
)

type VideoSource struct {
	URL  string
	Type VideoSourceType
}

type DownloadOptions struct {
	MaxParallel int
}

// UnknownTotal is passed as the totalBytes argument to OnProgress when the
// total size cannot be determined (e.g. HLS segment streams, curl downloads
// without Content-Length). The reporter should treat it as indeterminate.
const UnknownTotal int64 = -1

type ProgressReporter interface {
	OnEpisodeStart(ep Episode, index, total int)
	OnProgress(ep Episode, bytesDownloaded, totalBytes int64)
	OnEpisodeComplete(ep Episode, err error)
	OnAllComplete(succeeded, failed int)
}

type OutputFactory func(ctx context.Context, ep Episode) (io.WriteSeeker, func() error, error)
