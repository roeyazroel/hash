package autosuggest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tfcace/hash/internal/completion"
)

type speculativeCompletionProviderFunc func(context.Context, string, int) (completion.Result, error)

func (f speculativeCompletionProviderFunc) CompleteSpeculativeBounded(ctx context.Context, line string, pos int) (completion.Result, error) {
	return f(ctx, line, pos)
}

func TestCompletionStrategy_ReturnsOnlyStrictReconstructedExtension(t *testing.T) {
	strategy := CompletionStrategy{
		Router: speculativeCompletionProviderFunc(func(context.Context, string, int) (completion.Result, error) {
			return completion.Result{Prefix: "src/", Items: []completion.Item{{Value: "main.go"}}}, nil
		}),
		Timeout: time.Second,
	}

	got, err := strategy.Suggest(context.Background(), Request{Line: "cat src/ma", Cursor: len("cat src/ma")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != "cat src/main.go" {
		t.Errorf("Suggest() = %q, want cat src/main.go", got)
	}
}

func TestCompletionStrategy_RejectsReplacementsAndIgnoredCandidates(t *testing.T) {
	strategy := CompletionStrategy{
		Router: speculativeCompletionProviderFunc(func(context.Context, string, int) (completion.Result, error) {
			return completion.Result{Items: []completion.Item{{Value: "checkout"}}}, nil
		}),
		Timeout: time.Second,
		Ignore:  []string{"git checkout"},
	}

	got, err := strategy.Suggest(context.Background(), Request{Line: "git chek", Cursor: len("git chek")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != "" {
		t.Errorf("Suggest() = %q, want replacement to be rejected", got)
	}
}

func TestCompletionStrategy_PropagatesCancellation(t *testing.T) {
	strategy := CompletionStrategy{
		Router: speculativeCompletionProviderFunc(func(ctx context.Context, _ string, _ int) (completion.Result, error) {
			<-ctx.Done()
			return completion.Result{}, ctx.Err()
		}),
		Timeout: time.Second,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := strategy.Suggest(ctx, Request{Line: "git", Cursor: len("git")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Suggest() error = %v, want context.Canceled", err)
	}
}
