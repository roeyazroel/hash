package autosuggest

import "context"

// PreviousCommandProvider exposes prefix-aware learned sequence lookup.
type PreviousCommandProvider interface {
	SuggestCommandPrefix(context.Context, string, string, string) (string, error)
}

// MatchPreviousCommandStrategy suggests a command observed after the most
// recently executed command in the current directory.
type MatchPreviousCommandStrategy struct {
	Provider PreviousCommandProvider
	Ignore   []string
}

func (MatchPreviousCommandStrategy) Name() string { return "match_prev_cmd" }

func (s MatchPreviousCommandStrategy) Suggest(ctx context.Context, request Request) (string, error) {
	if s.Provider == nil || request.PreviousCommand == "" {
		return "", nil
	}
	candidate, err := s.Provider.SuggestCommandPrefix(ctx, request.PreviousCommand, request.Line, request.CWD)
	if err != nil {
		return "", err
	}
	if !isStrictExtension(request.Line, candidate) || MatchesAny(candidate, s.Ignore) {
		return "", nil
	}
	return candidate, nil
}
