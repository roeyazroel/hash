package autosuggest

import (
	"context"
	"errors"
	"testing"
)

type historyProviderFunc func(context.Context, string, int) ([]string, error)

func (f historyProviderFunc) SearchByPrefixContext(ctx context.Context, prefix string, limit int) ([]string, error) {
	return f(ctx, prefix, limit)
}

func TestHistoryStrategy_ReturnsFirstUsableUnignoredPrefix(t *testing.T) {
	strategy := HistoryStrategy{
		Store: historyProviderFunc(func(context.Context, string, int) ([]string, error) {
			return []string{"git", "git secret", "git status"}, nil
		}),
		Ignore: []string{"*secret*"},
	}

	got, err := strategy.Suggest(context.Background(), Request{Line: "git", Cursor: len("git")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != "git status" {
		t.Errorf("Suggest() = %q, want git status", got)
	}
}

func TestHistoryStrategy_PropagatesProviderError(t *testing.T) {
	want := errors.New("history unavailable")
	strategy := HistoryStrategy{Store: historyProviderFunc(func(context.Context, string, int) ([]string, error) { return nil, want })}
	_, err := strategy.Suggest(context.Background(), Request{Line: "git", Cursor: len("git")})
	if !errors.Is(err, want) {
		t.Fatalf("Suggest() error = %v, want %v", err, want)
	}
}
