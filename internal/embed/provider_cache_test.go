package embed

import (
	"context"
	"testing"
)

type countingProvider struct {
	model string
	calls int
}

func (p *countingProvider) Embed(_ context.Context, input string) ([]float32, error) {
	p.calls++
	if input == "hello" {
		return []float32{1, 2, 3}, nil
	}
	return []float32{4, 5, 6}, nil
}

func (p *countingProvider) ModelName() string {
	return p.model
}

func TestCachedProviderCachesByInput(t *testing.T) {
	base := &countingProvider{model: "test-model"}
	cached := NewCachedProvider(base)

	first, err := cached.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("first embed returned error: %v", err)
	}

	second, err := cached.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("second embed returned error: %v", err)
	}

	if base.calls != 1 {
		t.Fatalf("expected 1 provider call for repeated identical input, got %d", base.calls)
	}

	if len(first) != len(second) {
		t.Fatalf("embedding lengths differ: %d vs %d", len(first), len(second))
	}

	first[0] = 99
	if second[0] == 99 {
		t.Fatalf("cached embedding slices should be independent copies")
	}
}

func TestCachedProviderDoesNotMixInputs(t *testing.T) {
	base := &countingProvider{model: "test-model"}
	cached := NewCachedProvider(base)

	_, err := cached.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("embed hello returned error: %v", err)
	}

	_, err = cached.Embed(context.Background(), "world")
	if err != nil {
		t.Fatalf("embed world returned error: %v", err)
	}

	if base.calls != 2 {
		t.Fatalf("expected separate provider calls for distinct inputs, got %d", base.calls)
	}
}

func TestCachedProviderWithLimitEvictsLeastRecentlyUsed(t *testing.T) {
	base := &countingProvider{model: "test-model"}
	cached := NewCachedProviderWithLimit(base, 2)
	ctx := context.Background()

	if _, err := cached.Embed(ctx, "a"); err != nil {
		t.Fatalf("embed a failed: %v", err)
	}
	if _, err := cached.Embed(ctx, "b"); err != nil {
		t.Fatalf("embed b failed: %v", err)
	}

	// Touch "a" so "b" becomes the least recently used entry.
	if _, err := cached.Embed(ctx, "a"); err != nil {
		t.Fatalf("embed a (cached) failed: %v", err)
	}

	if _, err := cached.Embed(ctx, "c"); err != nil {
		t.Fatalf("embed c failed: %v", err)
	}

	if _, err := cached.Embed(ctx, "a"); err != nil {
		t.Fatalf("embed a (after eviction) failed: %v", err)
	}
	if _, err := cached.Embed(ctx, "b"); err != nil {
		t.Fatalf("embed b (recompute expected) failed: %v", err)
	}

	if base.calls != 4 {
		t.Fatalf("expected 4 provider calls with LRU eviction, got %d", base.calls)
	}
}
