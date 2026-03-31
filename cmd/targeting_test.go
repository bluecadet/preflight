package cmd

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/bluecadet/preflight/internal/targeting"
)

func TestRunHostsHonorsConcurrencyLimit(t *testing.T) {
	hosts := []targeting.ResolvedHost{
		{Name: "a"},
		{Name: "b"},
		{Name: "c"},
		{Name: "d"},
	}

	var mu sync.Mutex
	active := 0
	maxActive := 0

	err := runHosts(context.Background(), hosts, 2, func(_ context.Context, _ targeting.ResolvedHost) error {
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		active--
		mu.Unlock()
		return nil
	})
	if err != nil {
		t.Fatalf("runHosts returned error: %v", err)
	}
	if maxActive > 2 {
		t.Fatalf("expected max 2 concurrent hosts, saw %d", maxActive)
	}
}

func TestRunHostsPropagatesContextTimeout(t *testing.T) {
	hosts := []targeting.ResolvedHost{
		{Name: "a"},
		{Name: "b"},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	err := runHosts(ctx, hosts, 2, func(runCtx context.Context, _ targeting.ResolvedHost) error {
		select {
		case <-runCtx.Done():
			return runCtx.Err()
		case <-time.After(200 * time.Millisecond):
			return nil
		}
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
}
