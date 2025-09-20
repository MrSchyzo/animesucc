package animesucc

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

var (
	epNumberRe = regexp.MustCompile(`-ep-(\d+)$`)
	// Matches jwplayer file: "URL" or source src="URL" containing .m3u8 or .mp4
	jwplayerFileRe = regexp.MustCompile(`file:\s*"(https?://[^"]+\.(m3u8|mp4)[^"]*)"`)
)

func (c *Client) GetEpisodes(ctx context.Context, animeLink string) ([]Episode, error) {
	u := fmt.Sprintf("%s/anime/%s", c.baseURL, animeLink)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating anime page request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching anime page: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anime page returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing anime page HTML: %w", err)
	}

	seen := make(map[string]bool)
	var episodes []Episode

	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if !exists {
			return
		}
		if !strings.Contains(href, "/ep/") {
			return
		}
		if seen[href] {
			return
		}
		seen[href] = true

		// Extract slug from URL: last path segment
		parts := strings.Split(href, "/")
		slug := parts[len(parts)-1]

		// Parse episode number from slug
		matches := epNumberRe.FindStringSubmatch(slug)
		if matches == nil {
			return
		}
		num, err := strconv.Atoi(matches[1])
		if err != nil {
			return
		}

		episodes = append(episodes, Episode{
			Number: num,
			Slug:   slug,
			URL:    href,
		})
	})

	sort.Slice(episodes, func(i, j int) bool {
		return episodes[i].Number < episodes[j].Number
	})

	return episodes, nil
}

func (c *Client) GetVideoURL(ctx context.Context, episodeURL string) (*VideoSource, error) {
	// Step 1: fetch episode page to get the watch link
	watchURL, err := c.scrapeWatchLink(ctx, episodeURL)
	if err != nil {
		return nil, fmt.Errorf("scraping watch link: %w", err)
	}

	// Step 2: try primary player
	src, err := c.scrapeVideoSource(ctx, watchURL)
	if err == nil {
		return src, nil
	}

	// Step 3: try alternative player
	altURL := watchURL
	if strings.Contains(altURL, "?") {
		altURL += "&s=alt"
	} else {
		altURL += "?s=alt"
	}

	src, err = c.scrapeVideoSource(ctx, altURL)
	if err != nil {
		return nil, fmt.Errorf("could not find video URL in primary or alt player: %w", err)
	}
	return src, nil
}

func (c *Client) scrapeWatchLink(ctx context.Context, episodeURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, episodeURL, nil)
	if err != nil {
		return "", err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("episode page returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	var watchURL string
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if strings.Contains(href, "watch?file=") {
			watchURL = href
		}
	})

	if watchURL == "" {
		return "", fmt.Errorf("no watch link found on episode page %s", episodeURL)
	}

	return watchURL, nil
}

func (c *Client) scrapeVideoSource(ctx context.Context, watchURL string) (*VideoSource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, watchURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("watch page returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	html, err := doc.Html()
	if err != nil {
		return nil, err
	}

	// Try 1: jwplayer file: "URL" pattern in script tags
	if matches := jwplayerFileRe.FindStringSubmatch(html); matches != nil {
		return &VideoSource{
			URL:  matches[1],
			Type: classifyURL(matches[1]),
		}, nil
	}

	// Try 2: <source> element with src attribute
	var found *VideoSource
	doc.Find("source[src]").Each(func(_ int, s *goquery.Selection) {
		if found != nil {
			return
		}
		src, _ := s.Attr("src")
		if !strings.Contains(src, ".mp4") && !strings.Contains(src, ".m3u8") {
			return
		}
		found = &VideoSource{URL: src, Type: classifyURL(src)}
	})
	if found != nil {
		return found, nil
	}

	return nil, fmt.Errorf("no video source found on watch page %s", watchURL)
}

func classifyURL(u string) VideoSourceType {
	if strings.Contains(u, ".m3u8") {
		return VideoSourceM3U8
	}
	return VideoSourceMP4
}
