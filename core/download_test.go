package animesucc

import (
	"context"
	"crypto/sha256"
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
func (r *testProgressReporter) OnProgress(_ Episode, _, _ int64)            { r.progCalls.Add(1) }
func (r *testProgressReporter) OnEpisodeComplete(_ Episode, _ error)        { r.completed.Add(1) }
func (r *testProgressReporter) OnAllComplete(_, _ int)                      {}

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

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

func TestDownloadAll_SingleMP4(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c := NewClient()
	progress := &testProgressReporter{}

	// Short anime (~30MB episodes) for fast testing
	episodes := []Episode{
		{Number: 1, Slug: "Ani-ni-Tsukeru-Kusuri-wa-Nai-ep-1", URL: "https://www.animesaturn.cx/ep/Ani-ni-Tsukeru-Kusuri-wa-Nai-ep-1"},
	}

	err := c.DownloadAll(context.Background(), episodes, DownloadOptions{MaxParallel: 1}, fileOutputFactory(dir), progress)
	if err != nil {
		t.Fatalf("DownloadAll failed: %v", err)
	}

	path := filepath.Join(dir, "ep_001.mp4")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("output file not found: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
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

func TestDownloadAll_MP4DigestStable(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow digest-stability test")
	}
	t.Parallel()
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	c := NewClient()

	episodes := []Episode{
		{Number: 1, Slug: "Ani-ni-Tsukeru-Kusuri-wa-Nai-ep-1", URL: "https://www.animesaturn.cx/ep/Ani-ni-Tsukeru-Kusuri-wa-Nai-ep-1"},
	}

	noop := &testProgressReporter{}

	err := c.DownloadAll(context.Background(), episodes, DownloadOptions{MaxParallel: 1}, fileOutputFactory(dir1), noop)
	if err != nil {
		t.Fatalf("first download failed: %v", err)
	}
	err = c.DownloadAll(context.Background(), episodes, DownloadOptions{MaxParallel: 1}, fileOutputFactory(dir2), noop)
	if err != nil {
		t.Fatalf("second download failed: %v", err)
	}

	hash1, err := sha256File(filepath.Join(dir1, "ep_001.mp4"))
	if err != nil {
		t.Fatalf("hashing first file: %v", err)
	}
	hash2, err := sha256File(filepath.Join(dir2, "ep_001.mp4"))
	if err != nil {
		t.Fatalf("hashing second file: %v", err)
	}

	if hash1 != hash2 {
		t.Errorf("SHA-256 mismatch: %s vs %s", hash1, hash2)
	}
}
