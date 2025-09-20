package animesucc

import (
	"context"
	"testing"
)

func TestSearch_Naruto(t *testing.T) {
	t.Parallel()
	c := NewClient()
	results, err := c.Search(context.Background(), "naruto")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	foundNaruto := false
	for _, r := range results {
		if r.Name == "Naruto" {
			foundNaruto = true
		}
		if r.Link == "" {
			t.Errorf("result %q has empty link", r.Name)
		}
		if r.Name == "" {
			t.Error("result has empty name")
		}
	}
	if !foundNaruto {
		t.Error("expected to find an exact match for 'Naruto'")
	}
}

func TestSearch_NoResults(t *testing.T) {
	t.Parallel()
	c := NewClient()
	results, err := c.Search(context.Background(), "xyznonexistent999")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearch_Frieren(t *testing.T) {
	t.Parallel()
	c := NewClient()
	results, err := c.Search(context.Background(), "frieren")
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for frieren")
	}

	found := false
	for _, r := range results {
		if r.Name == "Frieren: Beyond Journey's End" {
			found = true
			if r.State == "" {
				t.Error("expected non-empty state")
			}
		}
	}
	if !found {
		t.Error("expected to find Frieren: Beyond Journey's End")
	}
}
