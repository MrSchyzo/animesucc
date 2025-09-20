package animesucc

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/grafov/m3u8"
	gomp4 "github.com/yapingcat/gomedia/go-mp4"
	"github.com/yapingcat/gomedia/go-mpeg2"
)

const hlsSegmentConcurrency = 4

type segResult struct {
	data []byte
	err  error
}

func (c *Client) downloadHLS(ctx context.Context, playlistURL string, w io.WriteSeeker, progressFn func(downloaded, total int64)) error {
	segments, err := c.resolveHLSSegments(ctx, playlistURL)
	if err != nil {
		return fmt.Errorf("resolving HLS segments: %w", err)
	}

	// Download all TS segments to a temp file using curl
	tsFile, err := os.CreateTemp("", "animesucc-hls-*.ts")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer os.Remove(tsFile.Name())
	defer tsFile.Close()

	n := len(segments)

	done := make([]chan segResult, n)
	for i := range done {
		done[i] = make(chan segResult, 1)
	}

	sem := make(chan struct{}, hlsSegmentConcurrency)
	var wg sync.WaitGroup
	var completedBytes atomic.Int64
	var completedSegments atomic.Int64

	for i, segURL := range segments {
		wg.Add(1)
		go func(idx int, u string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				done[idx] <- segResult{err: ctx.Err()}
				return
			default:
			}

			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				done[idx] <- segResult{err: ctx.Err()}
				return
			}
			defer func() { <-sem }()

			var buf bytes.Buffer
			if err := c.downloadSegment(ctx, u, &buf); err != nil {
				done[idx] <- segResult{err: err}
				return
			}

			downloaded := completedBytes.Add(int64(buf.Len()))
			finishedSegs := completedSegments.Add(1)
			progressFn(downloaded, downloaded/finishedSegs*int64(n))
			done[idx] <- segResult{data: buf.Bytes()}
		}(i, segURL)
	}

	var concatErr error
	for i := 0; i < n; i++ {
		res := <-done[i]
		if concatErr != nil {
			continue
		}
		if res.err != nil {
			concatErr = fmt.Errorf("downloading segment %d: %w", i, res.err)
			continue
		}
		if _, err := tsFile.Write(res.data); err != nil {
			concatErr = fmt.Errorf("writing segment %d: %w", i, err)
			continue
		}
	}

	wg.Wait()

	if concatErr != nil {
		return concatErr
	}

	tsFile.Close()

	// Remux TS -> MP4: prefer ffmpeg for better output, fall back to native Go
	if _, err := exec.LookPath("ffmpeg"); err == nil {
		return remuxWithFFmpeg(ctx, tsFile.Name(), w)
	}

	tsData, err := os.ReadFile(tsFile.Name())
	if err != nil {
		return fmt.Errorf("reading TS data: %w", err)
	}
	return remuxTSToMP4(tsData, w)
}

// remuxWithFFmpeg uses ffmpeg to remux a local TS file to MP4.
func remuxWithFFmpeg(ctx context.Context, tsPath string, w io.WriteSeeker) error {
	mp4File, err := os.CreateTemp("", "animesucc-hls-*.mp4")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	mp4Path := mp4File.Name()
	mp4File.Close()
	defer os.Remove(mp4Path)

	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-y",
		"-i", tsPath,
		"-c", "copy",
		mp4Path,
	)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg remux failed: %w\nstderr: %s", err, stderr.String())
	}

	src, err := os.Open(mp4Path)
	if err != nil {
		return err
	}
	defer src.Close()

	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("copying mp4 data: %w", err)
	}
	return nil
}

func (c *Client) resolveHLSSegments(ctx context.Context, playlistURL string) ([]string, error) {
	body, err := c.fetchURLWithFallback(ctx, playlistURL)
	if err != nil {
		return nil, err
	}

	playlist, listType, err := m3u8.DecodeFrom(bytes.NewReader(body), true)
	if err != nil {
		return nil, fmt.Errorf("parsing m3u8: %w", err)
	}

	baseURL, err := url.Parse(playlistURL)
	if err != nil {
		return nil, err
	}

	if listType == m3u8.MASTER {
		master := playlist.(*m3u8.MasterPlaylist)
		var best *m3u8.Variant
		for _, v := range master.Variants {
			if v == nil {
				continue
			}
			if best == nil || v.Bandwidth > best.Bandwidth {
				best = v
			}
		}
		if best == nil {
			return nil, fmt.Errorf("no variants in master playlist")
		}
		variantURL := resolveURL(baseURL, best.URI)
		return c.resolveHLSSegments(ctx, variantURL)
	}

	media := playlist.(*m3u8.MediaPlaylist)
	var segments []string
	for _, seg := range media.Segments {
		if seg == nil {
			continue
		}
		segments = append(segments, resolveURL(baseURL, seg.URI))
	}
	return segments, nil
}

func (c *Client) fetchURLWithFallback(ctx context.Context, u string) ([]byte, error) {
	if body, err := c.fetchURL(ctx, u); err == nil {
		return body, nil
	}
	return fetchWithCurl(ctx, u)
}

func (c *Client) downloadSegment(ctx context.Context, segURL string, w io.Writer) error {
	if err := c.downloadSegmentHTTP(ctx, segURL, w); err == nil {
		return nil
	}
	return downloadSegmentWithCurl(ctx, segURL, w)
}

func (c *Client) downloadSegmentHTTP(ctx context.Context, segURL string, w io.Writer) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, segURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", segURL, resp.StatusCode)
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// fetchWithCurl downloads a URL's content using curl (avoids Go TLS issues with CDN).
func fetchWithCurl(ctx context.Context, u string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "curl", "-sL", "--fail", u)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("curl fetch %s: %w", u, err)
	}
	return out, nil
}

// downloadSegmentWithCurl downloads a URL and appends to w.
func downloadSegmentWithCurl(ctx context.Context, segURL string, w io.Writer) error {
	data, err := fetchWithCurl(ctx, segURL)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func resolveURL(base *url.URL, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	refURL, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return base.ResolveReference(refURL).String()
}

func remuxTSToMP4(tsData []byte, w io.WriteSeeker) error {
	muxer, err := gomp4.CreateMp4Muxer(w)
	if err != nil {
		return fmt.Errorf("creating mp4 muxer: %w", err)
	}

	hasVideo := false
	hasAudio := false
	var videoTrack, audioTrack uint32

	demuxer := mpeg2.NewTSDemuxer()
	demuxer.OnFrame = func(cid mpeg2.TS_STREAM_TYPE, frame []byte, pts uint64, dts uint64) {
		switch cid {
		case mpeg2.TS_STREAM_H264:
			if !hasVideo {
				videoTrack = muxer.AddVideoTrack(gomp4.MP4_CODEC_H264)
				hasVideo = true
			}
			muxer.Write(videoTrack, frame, pts, dts)
		case mpeg2.TS_STREAM_H265:
			if !hasVideo {
				videoTrack = muxer.AddVideoTrack(gomp4.MP4_CODEC_H265)
				hasVideo = true
			}
			muxer.Write(videoTrack, frame, pts, dts)
		case mpeg2.TS_STREAM_AAC:
			if !hasAudio {
				audioTrack = muxer.AddAudioTrack(gomp4.MP4_CODEC_AAC)
				hasAudio = true
			}
			muxer.Write(audioTrack, frame, pts, dts)
		case mpeg2.TS_STREAM_AUDIO_MPEG1, mpeg2.TS_STREAM_AUDIO_MPEG2:
			if !hasAudio {
				audioTrack = muxer.AddAudioTrack(gomp4.MP4_CODEC_MP3)
				hasAudio = true
			}
			muxer.Write(audioTrack, frame, pts, dts)
		}
	}

	if err := demuxer.Input(bytes.NewReader(tsData)); err != nil {
		return fmt.Errorf("demuxing TS data: %w", err)
	}

	if err := muxer.WriteTrailer(); err != nil {
		return fmt.Errorf("writing mp4 trailer: %w", err)
	}

	return nil
}
