package autosuggest

import (
	"context"
	"testing"
)

type previousCommandProviderFunc func(context.Context, string, string, string) (string, error)

func (f previousCommandProviderFunc) SuggestCommandPrefix(ctx context.Context, previous, prefix, cwd string) (string, error) {
	return f(ctx, previous, prefix, cwd)
}

func TestMatchPreviousCommandStrategy_UsesRequestContext(t *testing.T) {
	var gotPrevious, gotPrefix, gotCWD string
	strategy := MatchPreviousCommandStrategy{Provider: previousCommandProviderFunc(func(_ context.Context, previous, prefix, cwd string) (string, error) {
		gotPrevious, gotPrefix, gotCWD = previous, prefix, cwd
		return "git status", nil
	})}

	got, err := strategy.Suggest(context.Background(), Request{
		Line:            "git",
		Cursor:          len("git"),
		PreviousCommand: "git pull origin main",
		CWD:             "/project",
	})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != "git status" || gotPrevious != "git pull origin main" || gotPrefix != "git" || gotCWD != "/project" {
		t.Errorf("Suggest() = %q; provider args = %q, %q, %q", got, gotPrevious, gotPrefix, gotCWD)
	}
}

func TestMatchPreviousCommandStrategy_SkipsEmptyPreviousCommand(t *testing.T) {
	called := false
	strategy := MatchPreviousCommandStrategy{Provider: previousCommandProviderFunc(func(context.Context, string, string, string) (string, error) {
		called = true
		return "git status", nil
	})}

	got, err := strategy.Suggest(context.Background(), Request{Line: "git", Cursor: len("git")})
	if err != nil || got != "" || called {
		t.Errorf("Suggest() = %q, %v; called = %v", got, err, called)
	}
}
