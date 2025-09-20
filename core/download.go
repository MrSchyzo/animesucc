package animesucc

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

func (c *Client) DownloadAll(ctx context.Context, episodes []Episode, opts DownloadOptions, output OutputFactory, progress ProgressReporter) error {
	if opts.MaxParallel <= 0 {
		opts.MaxParallel = 1
	}

	sem := make(chan struct{}, opts.MaxParallel)
	var wg sync.WaitGroup
	var succeeded, failed atomic.Int32
	var mu sync.Mutex
	var firstErr error

	for i, ep := range episodes {
		wg.Add(1)
		go func(idx int, ep Episode) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			progress.OnEpisodeStart(ep, idx, len(episodes))

			err := c.downloadEpisode(ctx, ep, output, progress)
			if err != nil {
				failed.Add(1)
				mu.Lock()
				if firstErr == nil {
					firstErr = fmt.Errorf("ep %d: %w", ep.Number, err)
				}
				mu.Unlock()
			} else {
				succeeded.Add(1)
			}
			progress.OnEpisodeComplete(ep, err)
		}(i, ep)
	}

	wg.Wait()
	progress.OnAllComplete(int(succeeded.Load()), int(failed.Load()))

	if f := failed.Load(); f > 0 {
		return fmt.Errorf("%d episode(s) failed to download; first error: %w", f, firstErr)
	}
	return nil
}

func (c *Client) downloadEpisode(ctx context.Context, ep Episode, output OutputFactory, progress ProgressReporter) error {
	src, err := c.GetVideoURL(ctx, ep.URL)
	if err != nil {
		return fmt.Errorf("resolving video URL for ep %d: %w", ep.Number, err)
	}

	w, cleanup, err := output(ctx, ep)
	if err != nil {
		return fmt.Errorf("creating output for ep %d: %w", ep.Number, err)
	}
	defer cleanup()

	progressFn := func(downloaded, total int64) {
		progress.OnProgress(ep, downloaded, total)
	}

	switch src.Type {
	case VideoSourceMP4:
		return c.downloadFile(ctx, src.URL, w, progressFn)
	case VideoSourceM3U8:
		return c.downloadHLS(ctx, src.URL, w, progressFn)
	default:
		return fmt.Errorf("unknown video source type %q", src.Type)
	}
}

func (c *Client) downloadFile(ctx context.Context, url string, w io.WriteSeeker, progressFn func(downloaded, total int64)) error {
	if err := c.downloadFileHTTP(ctx, url, w, progressFn); err == nil {
		return nil
	} else {
		log.Printf("[WARN] HTTP download failed (%v), falling back to curl", err)
	}
	return downloadWithCurl(ctx, url, w, progressFn)
}

func (c *Client) downloadFileHTTP(ctx context.Context, url string, w io.Writer, progressFn func(downloaded, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, resp.StatusCode)
	}

	total := resp.ContentLength // -1 if unknown
	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				return fmt.Errorf("writing data: %w", writeErr)
			}
			downloaded += int64(n)
			progressFn(downloaded, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return fmt.Errorf("reading response body: %w", readErr)
		}
	}
	return nil
}

// downloadWithCurl uses curl to download a URL, writing output to w.
// This avoids Go's TLS incompatibility with certain CDN servers.
// Progress is reported live by piping curl's stdout through our read loop.
func downloadWithCurl(ctx context.Context, url string, w io.WriteSeeker, progressFn func(downloaded, total int64)) error {
	total := getContentLengthWithCurl(ctx, url)

	cmd := exec.CommandContext(ctx, "curl", "-sL", "--fail", "-o", "-", url)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("creating stdout pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting curl: %w", err)
	}

	buf := make([]byte, 32*1024)
	var downloaded int64
	for {
		n, readErr := stdout.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				_ = cmd.Process.Kill()
				return fmt.Errorf("writing data: %w", writeErr)
			}
			downloaded += int64(n)
			progressFn(downloaded, total)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			return fmt.Errorf("reading curl output: %w", readErr)
		}
	}

	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("curl download failed: %w", err)
	}
	return nil
}

// getContentLengthWithCurl does a HEAD request via curl and returns the Content-Length,
// or -1 if it cannot be determined.
func getContentLengthWithCurl(ctx context.Context, url string) int64 {
	cmd := exec.CommandContext(ctx, "curl", "-sI", "--fail", url)
	out, err := cmd.Output()
	if err != nil {
		return -1
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		lower := strings.ToLower(strings.TrimSpace(line))
		if strings.HasPrefix(lower, "content-length:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				var size int64
				if _, scanErr := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &size); scanErr == nil {
					return size
				}
			}
		}
	}
	return -1
}
