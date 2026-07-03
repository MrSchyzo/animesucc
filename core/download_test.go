package animesucc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type testProgressReporter struct {
	started   atomic.Int32
	completed atomic.Int32
	progCalls atomic.Int64
}

func (r *testProgressReporter) OnEpisodeStart(_ Episode, _, _ int) { r.started.Add(1) }
func (r *testProgressReporter) OnProgress(_ Episode, _, _ int64)   { r.progCalls.Add(1) }
func (r *testProgressReporter) OnEpisodeComplete(_ Episode, _ error) {
	r.completed.Add(1)
}
func (r *testProgressReporter) OnAllComplete(_, _ int) {}

func fileOutputFactory(dir string) OutputFactory {
	return func(_ context.Context, ep Episode) (io.WriteSeeker, func() error, error) {
		path := filepath.Join(dir, fmt.Sprintf("ep_%03d.mp4", ep.Number))
		f, err := os.Create(path)
		if err != nil {
			return nil, nil, err
		}
		return f, f.Close, nil
	}
}

// TestDownloadAll_SingleMP4 exercises the full resolve-then-download path offline:
// GetVideoURL (player -> embed -> playlist -> decode) yields an mp4 URL served by the
// test server, and downloadFile streams it to disk.
func TestDownloadAll_SingleMP4(t *testing.T) {
	t.Parallel()
	body := bytes.Repeat([]byte("SUCC"), 40*1024) // ~160 KiB -> multiple progress ticks
	srv := newSaturnServer(t, "mp4", body)
	c := NewClient(WithBaseURL(srv.URL))
	dir := t.TempDir()
	progress := &testProgressReporter{}

	episodes := []Episode{
		{Number: 1, Slug: "ep-1", URL: srv.URL + "/anime/naruto-iN621/ep-1"},
	}

	err := c.DownloadAll(context.Background(), episodes, DownloadOptions{MaxParallel: 1}, fileOutputFactory(dir), progress)
	if err != nil {
		t.Fatalf("DownloadAll failed: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "ep_001.mp4"))
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() != int64(len(body)) {
		t.Errorf("output size = %d, want %d", info.Size(), len(body))
	}
	if progress.started.Load() != 1 {
		t.Errorf("expected 1 start callback, got %d", progress.started.Load())
	}
	if progress.completed.Load() != 1 {
		t.Errorf("expected 1 complete callback, got %d", progress.completed.Load())
	}
	if progress.progCalls.Load() == 0 {
		t.Error("expected at least one progress callback")
	}
}
