package animesucc

import (
	"fmt"
	"strconv"
	"strings"
)

type EpisodeFilter struct {
	episodes map[int]bool
	all      bool
}

func ParseEpisodeFilter(s string) (EpisodeFilter, error) {
	if s == "" {
		return EpisodeFilter{all: true}, nil
	}

	eps := make(map[int]bool)
	parts := strings.Split(s, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return EpisodeFilter{}, fmt.Errorf("empty segment in episode filter %q", s)
		}

		if !strings.Contains(part, "-") {
			n, err := strconv.Atoi(part)
			if err != nil {
				return EpisodeFilter{}, fmt.Errorf("invalid episode number %q: %w", part, err)
			}
			eps[n] = true
			continue
		}

		bounds := strings.SplitN(part, "-", 2)
		if bounds[0] == "" || bounds[1] == "" {
			return EpisodeFilter{}, fmt.Errorf("invalid range %q in episode filter", part)
		}

		start, err := strconv.Atoi(bounds[0])
		if err != nil {
			return EpisodeFilter{}, fmt.Errorf("invalid number %q in range: %w", bounds[0], err)
		}
		end, err := strconv.Atoi(bounds[1])
		if err != nil {
			return EpisodeFilter{}, fmt.Errorf("invalid number %q in range: %w", bounds[1], err)
		}
		if start > end {
			return EpisodeFilter{}, fmt.Errorf("invalid range %q: start > end", part)
		}

		for i := start; i <= end; i++ {
			eps[i] = true
		}
	}

	return EpisodeFilter{episodes: eps}, nil
}

func (f EpisodeFilter) Includes(episode int) bool {
	if f.all {
		return true
	}
	return f.episodes[episode]
}

func (f EpisodeFilter) Apply(episodes []Episode) []Episode {
	if f.all {
		result := make([]Episode, len(episodes))
		copy(result, episodes)
		return result
	}

	var result []Episode
	for _, ep := range episodes {
		if f.episodes[ep.Number] {
			result = append(result, ep)
		}
	}
	return result
}
