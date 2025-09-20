package animesucc

import (
	"testing"
)

func TestParseEpisodeFilter_SingleNumber(t *testing.T) {
	t.Parallel()
	f, err := ParseEpisodeFilter("5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !f.Includes(5) {
		t.Error("expected filter to include 5")
	}
	if f.Includes(4) {
		t.Error("expected filter to not include 4")
	}
}

func TestParseEpisodeFilter_Range(t *testing.T) {
	t.Parallel()
	f, err := ParseEpisodeFilter("1-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 1; i <= 5; i++ {
		if !f.Includes(i) {
			t.Errorf("expected filter to include %d", i)
		}
	}
	if f.Includes(0) {
		t.Error("expected filter to not include 0")
	}
	if f.Includes(6) {
		t.Error("expected filter to not include 6")
	}
}

func TestParseEpisodeFilter_Mixed(t *testing.T) {
	t.Parallel()
	f, err := ParseEpisodeFilter("1-3,7,10-12")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := map[int]bool{1: true, 2: true, 3: true, 7: true, 10: true, 11: true, 12: true}
	for ep := 0; ep <= 15; ep++ {
		got := f.Includes(ep)
		want := expected[ep]
		if got != want {
			t.Errorf("Includes(%d) = %v, want %v", ep, got, want)
		}
	}
}

func TestParseEpisodeFilter_Empty(t *testing.T) {
	t.Parallel()
	f, err := ParseEpisodeFilter("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty filter means "all episodes"
	for _, ep := range []int{1, 50, 100, 999} {
		if !f.Includes(ep) {
			t.Errorf("empty filter should include %d", ep)
		}
	}
}

func TestParseEpisodeFilter_InvalidInput(t *testing.T) {
	t.Parallel()
	cases := []string{"abc", "1-", "-5", "1-2-3", "1,,2", ",", "5-3"}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			_, err := ParseEpisodeFilter(input)
			if err == nil {
				t.Errorf("expected error for input %q", input)
			}
		})
	}
}

func TestEpisodeFilter_Apply(t *testing.T) {
	t.Parallel()
	episodes := []Episode{
		{Number: 1, Slug: "ep-1"},
		{Number: 2, Slug: "ep-2"},
		{Number: 3, Slug: "ep-3"},
		{Number: 5, Slug: "ep-5"},
		{Number: 8, Slug: "ep-8"},
		{Number: 10, Slug: "ep-10"},
	}

	f, err := ParseEpisodeFilter("2-3,8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := f.Apply(episodes)
	if len(result) != 3 {
		t.Fatalf("expected 3 episodes, got %d", len(result))
	}
	if result[0].Number != 2 || result[1].Number != 3 || result[2].Number != 8 {
		t.Errorf("unexpected episodes: %v", result)
	}
}

func TestEpisodeFilter_ApplyAll(t *testing.T) {
	t.Parallel()
	episodes := []Episode{
		{Number: 1, Slug: "ep-1"},
		{Number: 2, Slug: "ep-2"},
		{Number: 3, Slug: "ep-3"},
	}

	f, err := ParseEpisodeFilter("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := f.Apply(episodes)
	if len(result) != 3 {
		t.Fatalf("empty filter should return all episodes, got %d", len(result))
	}
}
