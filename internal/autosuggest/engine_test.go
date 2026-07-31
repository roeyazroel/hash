package autosuggest

import (
	"context"
	"errors"
	"testing"
)

type strategyFunc struct {
	name string
	fn   func(context.Context, Request) (string, error)
}

func (s strategyFunc) Name() string { return s.name }

func (s strategyFunc) Suggest(ctx context.Context, request Request) (string, error) {
	return s.fn(ctx, request)
}

func TestEngine_UsesFirstValidCandidateInOrder(t *testing.T) {
	calledThird := false
	engine := NewEngine(Config{}, []Strategy{
		strategyFunc{name: "miss", fn: func(context.Context, Request) (string, error) { return "", nil }},
		strategyFunc{name: "history", fn: func(context.Context, Request) (string, error) { return "git status", nil }},
		strategyFunc{name: "later", fn: func(context.Context, Request) (string, error) {
			calledThird = true
			return "git push", nil
		}},
	})

	got, err := engine.Suggest(context.Background(), Request{Line: "git", Cursor: len("git")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != (Candidate{Text: "git status", Strategy: "history"}) {
		t.Errorf("Suggest() = %+v, want history candidate", got)
	}
	if calledThird {
		t.Error("engine called a strategy after a valid candidate")
	}
}

func TestEngine_FallsThroughMissesErrorsAndInvalidCandidates(t *testing.T) {
	engine := NewEngine(Config{}, []Strategy{
		strategyFunc{name: "error", fn: func(context.Context, Request) (string, error) { return "", errors.New("unavailable") }},
		strategyFunc{name: "equal", fn: func(context.Context, Request) (string, error) { return "git", nil }},
		strategyFunc{name: "replacement", fn: func(context.Context, Request) (string, error) { return "go test", nil }},
		strategyFunc{name: "valid", fn: func(context.Context, Request) (string, error) { return "git status", nil }},
	})

	got, err := engine.Suggest(context.Background(), Request{Line: "git", Cursor: len("git")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got != (Candidate{Text: "git status", Strategy: "valid"}) {
		t.Errorf("Suggest() = %+v, want valid candidate", got)
	}
}

func TestEngine_CancellationStopsTraversal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calledNext := false
	engine := NewEngine(Config{}, []Strategy{
		strategyFunc{name: "blocking", fn: func(ctx context.Context, _ Request) (string, error) {
			cancel()
			<-ctx.Done()
			return "", ctx.Err()
		}},
		strategyFunc{name: "next", fn: func(context.Context, Request) (string, error) {
			calledNext = true
			return "git status", nil
		}},
	})

	_, err := engine.Suggest(ctx, Request{Line: "git", Cursor: len("git")})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Suggest() error = %v, want context.Canceled", err)
	}
	if calledNext {
		t.Error("engine continued after cancellation")
	}
}

func TestEngine_RejectsIneligibleRequestsUsingRuneCounts(t *testing.T) {
	called := false
	engine := NewEngine(Config{MinInputLength: 2, MaxBufferSize: 3}, []Strategy{
		strategyFunc{name: "history", fn: func(context.Context, Request) (string, error) {
			called = true
			return "git status", nil
		}},
	})

	tests := []Request{
		{Line: "g", Cursor: 1},
		{Line: "git", Cursor: 2},
		{Line: "日本語語", Cursor: len("日本語語")},
	}
	for _, request := range tests {
		got, err := engine.Suggest(context.Background(), request)
		if err != nil {
			t.Fatalf("Suggest(%+v) error = %v", request, err)
		}
		if got != (Candidate{}) {
			t.Errorf("Suggest(%+v) = %+v, want no candidate", request, got)
		}
	}
	if called {
		t.Error("engine called a strategy for an ineligible request")
	}
}

func TestMatchesAnyUsesSimpleWildcardSemantics(t *testing.T) {
	tests := []struct {
		value    string
		patterns []string
		want     bool
	}{
		{value: "git status", patterns: []string{"git *"}, want: true},
		{value: "git/status", patterns: []string{"git/*"}, want: true},
		{value: "git push", patterns: []string{"git p?sh"}, want: true},
		{value: "git status", patterns: []string{"git p?sh"}, want: false},
	}
	for _, tt := range tests {
		if got := MatchesAny(tt.value, tt.patterns); got != tt.want {
			t.Errorf("MatchesAny(%q, %v) = %v, want %v", tt.value, tt.patterns, got, tt.want)
		}
	}
}
