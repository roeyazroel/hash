package autosuggest

import "context"

// HistoryProvider is the small history-store surface needed by suggestions.
type HistoryProvider interface {
	SearchByPrefixContext(context.Context, string, int) ([]string, error)
}

// HistoryStrategy returns the most recent non-ignored history extension.
type HistoryStrategy struct {
	Store  HistoryProvider
	Ignore []string
	Limit  int
}

func (HistoryStrategy) Name() string { return "history" }

func (s HistoryStrategy) Suggest(ctx context.Context, request Request) (string, error) {
	if s.Store == nil {
		return "", nil
	}
	limit := s.Limit
	if limit <= 0 {
		limit = 20
	}
	candidates, err := s.Store.SearchByPrefixContext(ctx, request.Line, limit)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		if isStrictExtension(request.Line, candidate) && !MatchesAny(candidate, s.Ignore) {
			return candidate, nil
		}
	}
	return "", nil
}
