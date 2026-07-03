package animesucc

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Search queries AnimeSaturn's /filter page and parses the HTML result cards.
//
// The pre-2026 site exposed a JSON endpoint at /index.php?search=1&key=; the revamp
// removed it (it now 404s) in favour of a server-rendered /filter page.
func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error) {
	u := fmt.Sprintf("%s/filter?key=%s", c.baseURL, url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("creating search request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("executing search request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parsing search HTML: %w", err)
	}

	var results []SearchResult
	// Result cards are <a class="ac">; the hero/"In evidenza" slider also links to
	// /anime/... but uses .hero-btn, so scoping to a.ac excludes it. A query with no
	// matches yields zero a.ac cards ("Nessun risultato") -> empty slice, not an error.
	doc.Find("a.ac").Each(func(_ int, s *goquery.Selection) {
		href, ok := s.Attr("href")
		if !ok || !strings.Contains(href, "/anime/") {
			return
		}
		image, _ := s.Find(".ac__poster img").First().Attr("src")
		results = append(results, SearchResult{
			Name:    strings.TrimSpace(s.Find(".ac__title").First().Text()),
			Link:    strings.TrimPrefix(strings.TrimPrefix(href, c.baseURL), "/anime/"),
			Image:   image,
			Release: strings.TrimSpace(s.Find(".ac__sub").First().Text()),
			Type:    strings.TrimSpace(s.Find(".ac__type-badge").First().Text()),
		})
	})

	return results, nil
}
