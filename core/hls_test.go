package animesucc

import (
	"bytes"
	"context"
	"os"
	"testing"
)

func isValidMP4(data []byte) bool {
	// MP4 files start with a box header; the first box is typically "ftyp"
	if len(data) < 8 {
		return false
	}
	boxType := string(data[4:8])
	return boxType == "ftyp" || boxType == "moov" || boxType == "free"
}

func TestDownloadHLS(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping slow HLS download test")
	}
	t.Parallel()

	c := NewClient()
	src, err := c.GetVideoURL(context.Background(), "https://www.animesaturn.cx/ep/Naruto-ep-1")
	if err != nil {
		t.Fatalf("GetVideoURL: %v", err)
	}
	if src.Type != VideoSourceM3U8 {
		t.Fatalf("expected m3u8 source, got %s", src.Type)
	}

	tmpFile, err := os.CreateTemp("", "hls-test-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	var progressCalls int
	progressFn := func(downloaded, total int64) {
		progressCalls++
	}

	err = c.downloadHLS(context.Background(), src.URL, tmpFile, progressFn)
	if err != nil {
		t.Fatalf("downloadHLS: %v", err)
	}

	info, err := tmpFile.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("output file is empty")
	}
	t.Logf("HLS download: %d bytes, %d progress calls", info.Size(), progressCalls)

	tmpFile.Seek(0, 0)
	header := make([]byte, 8)
	tmpFile.Read(header)
	if !isValidMP4(header) {
		t.Errorf("output is not valid MP4, header: %x", header)
	}
}

func TestResolveHLSSegments(t *testing.T) {
	t.Parallel()

	c := NewClient()
	src, err := c.GetVideoURL(context.Background(), "https://www.animesaturn.cx/ep/Naruto-ep-1")
	if err != nil {
		t.Fatalf("GetVideoURL: %v", err)
	}
	if src.Type != VideoSourceM3U8 {
		t.Fatalf("expected m3u8 source, got %s", src.Type)
	}

	segments, err := c.resolveHLSSegments(context.Background(), src.URL)
	if err != nil {
		t.Fatalf("resolveHLSSegments: %v", err)
	}

	if len(segments) == 0 {
		t.Fatal("no segments found")
	}
	t.Logf("found %d segments", len(segments))

	for i, s := range segments {
		if len(s) < 8 || (s[:7] != "http://" && s[:8] != "https://") {
			t.Errorf("segment %d is not absolute URL: %s", i, s)
		}
	}
}

func TestRemuxTSToMP4(t *testing.T) {
	t.Parallel()

	c := NewClient()
	src, err := c.GetVideoURL(context.Background(), "https://www.animesaturn.cx/ep/Naruto-ep-1")
	if err != nil {
		t.Fatalf("GetVideoURL: %v", err)
	}

	segments, err := c.resolveHLSSegments(context.Background(), src.URL)
	if err != nil {
		t.Fatalf("resolveHLSSegments: %v", err)
	}
	if len(segments) == 0 {
		t.Fatal("no segments")
	}

	// Download just the first segment
	tsData, err := fetchWithCurl(context.Background(), segments[0])
	if err != nil {
		t.Fatalf("fetching segment: %v", err)
	}

	// Remux with gomedia
	var buf bytes.Buffer
	ws := &seekableBuffer{buf: &buf}
	err = remuxTSToMP4(tsData, ws)
	if err != nil {
		t.Fatalf("remuxTSToMP4: %v", err)
	}

	if buf.Len() == 0 {
		t.Fatal("remuxed output is empty")
	}
	t.Logf("remuxed %d TS bytes -> %d MP4 bytes", len(tsData), buf.Len())
}

func TestRemuxWithFFmpeg(t *testing.T) {
	t.Parallel()

	c := NewClient()
	src, err := c.GetVideoURL(context.Background(), "https://www.animesaturn.cx/ep/Naruto-ep-1")
	if err != nil {
		t.Fatalf("GetVideoURL: %v", err)
	}

	segments, err := c.resolveHLSSegments(context.Background(), src.URL)
	if err != nil {
		t.Fatalf("resolveHLSSegments: %v", err)
	}
	if len(segments) < 3 {
		t.Fatal("expected at least 3 segments")
	}

	// Download first few segments into a TS file
	tsFile, err := os.CreateTemp("", "ffmpeg-test-*.ts")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tsFile.Name())

	for _, segURL := range segments[:3] {
		if err := downloadSegmentWithCurl(context.Background(), segURL, tsFile); err != nil {
			t.Fatalf("downloading segment: %v", err)
		}
	}
	tsFile.Close()

	// Remux with ffmpeg
	mp4File, err := os.CreateTemp("", "ffmpeg-test-*.mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(mp4File.Name())
	defer mp4File.Close()

	err = remuxWithFFmpeg(context.Background(), tsFile.Name(), mp4File)
	if err != nil {
		t.Fatalf("remuxWithFFmpeg: %v", err)
	}

	info, err := mp4File.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() == 0 {
		t.Fatal("ffmpeg output is empty")
	}
	t.Logf("ffmpeg remuxed -> %d MP4 bytes", info.Size())

	mp4File.Seek(0, 0)
	header := make([]byte, 8)
	mp4File.Read(header)
	if !isValidMP4(header) {
		t.Errorf("not valid MP4, header: %x", header)
	}
}

// seekableBuffer wraps bytes.Buffer to satisfy io.WriteSeeker
type seekableBuffer struct {
	buf *bytes.Buffer
	pos int
}

func (s *seekableBuffer) Write(p []byte) (int, error) {
	for s.buf.Len() < s.pos+len(p) {
		s.buf.WriteByte(0)
	}
	copy(s.buf.Bytes()[s.pos:], p)
	s.pos += len(p)
	return len(p), nil
}

func (s *seekableBuffer) Seek(offset int64, whence int) (int64, error) {
	var newPos int
	switch whence {
	case 0:
		newPos = int(offset)
	case 1:
		newPos = s.pos + int(offset)
	case 2:
		newPos = s.buf.Len() + int(offset)
	}
	if newPos < 0 {
		newPos = 0
	}
	s.pos = newPos
	return int64(newPos), nil
}
