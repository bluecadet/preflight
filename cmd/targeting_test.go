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

func TestRunHostsContinuesAfterHostError(t *testing.T) {
	hosts := []targeting.ResolvedHost{
		{Name: "a"},
		{Name: "b"},
	}

	var mu sync.Mutex
	visited := make(map[string]bool)
	errBoom := errors.New("boom")

	err := runHosts(context.Background(), hosts, 1, func(_ context.Context, host targeting.ResolvedHost) error {
		mu.Lock()
		visited[host.Name] = true
		mu.Unlock()
		if host.Name == "a" {
			return errBoom
		}
		return nil
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("expected joined host error, got %v", err)
	}
	for _, host := range hosts {
		if !visited[host.Name] {
			t.Fatalf("expected host %q to run after peer failure", host.Name)
		}
	}
}

func TestMergeSelectors(t *testing.T) {
	cases := []struct {
		name       string
		flagValues []string
		positional []string
		want       []string
	}{
		{
			name:       "positional_only",
			positional: []string{"host-a", "host-b"},
			want:       []string{"host-a", "host-b"},
		},
		{
			name:       "flag_only",
			flagValues: []string{"host-a"},
			want:       []string{"host-a"},
		},
		{
			name:       "merged_no_overlap",
			flagValues: []string{"host-a"},
			positional: []string{"host-b"},
			want:       []string{"host-a", "host-b"},
		},
		{
			name:       "deduplication",
			flagValues: []string{"host-a"},
			positional: []string{"host-a", "host-b"},
			want:       []string{"host-a", "host-b"},
		},
		{
			name: "empty",
			want: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeSelectors(tc.flagValues, tc.positional)
			if len(got) != len(tc.want) {
				t.Fatalf("mergeSelectors() = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("mergeSelectors()[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
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
