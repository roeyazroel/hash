package autosuggest

import (
	"context"
	"time"

	"github.com/tfcace/hash/internal/completion"
)

// SpeculativeCompletionProvider is the restricted router surface used while a
// user is typing. Implementations must exclude agent and remote providers.
type SpeculativeCompletionProvider interface {
	CompleteSpeculativeBounded(context.Context, string, int) (completion.Result, error)
}

// CompletionStrategy converts the first restricted completion result into a
// full-line candidate. Replacement-style completions are rejected unless they
// are an exact extension of the typed line.
type CompletionStrategy struct {
	Router  SpeculativeCompletionProvider
	Ignore  []string
	Timeout time.Duration
}

func (CompletionStrategy) Name() string { return "completion" }

func (s CompletionStrategy) Suggest(ctx context.Context, request Request) (string, error) {
	if s.Router == nil {
		return "", nil
	}
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 150 * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := s.Router.CompleteSpeculativeBounded(ctx, request.Line, request.Cursor)
	if err != nil {
		return "", err
	}
	if len(result.Items) == 0 {
		return "", nil
	}
	candidate, _, ok := completion.ReconstructLine(request.Line, request.Cursor, result, result.Items[0])
	if !ok || !isStrictExtension(request.Line, candidate) || MatchesAny(candidate, s.Ignore) {
		return "", nil
	}
	return candidate, nil
}
