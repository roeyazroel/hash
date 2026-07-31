package autosuggest

import (
	"context"
	"strings"
	"unicode/utf8"
)

// Engine runs ordered strategies and returns the first exact extension.
type Engine struct {
	config     Config
	strategies []Strategy
}

// NewEngine creates an ordered autosuggestion engine. The slice order is part
// of the product contract: the first valid strategy wins.
func NewEngine(config Config, strategies []Strategy) *Engine {
	return &Engine{config: config, strategies: strategies}
}

// Suggest checks eligibility and traverses strategies until one returns a
// strict extension of the exact input line. Ordinary strategy errors are
// reported through Config.OnError and do not block later local strategies.
func (e *Engine) Suggest(ctx context.Context, request Request) (Candidate, error) {
	if err := ctx.Err(); err != nil {
		return Candidate{}, err
	}
	if !e.eligible(request) {
		return Candidate{}, nil
	}

	for _, strategy := range e.strategies {
		if err := ctx.Err(); err != nil {
			return Candidate{}, err
		}
		text, err := strategy.Suggest(ctx, request)
		if err != nil {
			if ctx.Err() != nil {
				return Candidate{}, ctx.Err()
			}
			if e.config.OnError != nil {
				e.config.OnError(strategy, err)
			}
			continue
		}
		if isStrictExtension(request.Line, text) {
			return Candidate{Text: text, Strategy: strategy.Name()}, nil
		}
	}
	return Candidate{}, nil
}

func (e *Engine) eligible(request Request) bool {
	if request.Cursor != len(request.Line) {
		return false
	}
	runes := utf8.RuneCountInString(request.Line)
	if runes < e.config.MinInputLength {
		return false
	}
	return e.config.MaxBufferSize == 0 || runes <= e.config.MaxBufferSize
}

func isStrictExtension(input, candidate string) bool {
	return candidate != input && strings.HasPrefix(candidate, input)
}

// MatchesAny matches value against a list of simple wildcard patterns. Only
// '*' (any sequence) and '?' (one rune) are special, including across '/'.
func MatchesAny(value string, patterns []string) bool {
	for _, pattern := range patterns {
		if matchWildcard([]rune(pattern), []rune(value)) {
			return true
		}
	}
	return false
}

func matchWildcard(pattern, value []rune) bool {
	type position struct{ pattern, value int }
	memo := make(map[position]bool)
	seen := make(map[position]bool)
	var match func(int, int) bool
	match = func(patternPos, valuePos int) bool {
		pos := position{patternPos, valuePos}
		if seen[pos] {
			return memo[pos]
		}
		seen[pos] = true

		var result bool
		switch {
		case patternPos == len(pattern):
			result = valuePos == len(value)
		case pattern[patternPos] == '*':
			result = match(patternPos+1, valuePos) || (valuePos < len(value) && match(patternPos, valuePos+1))
		case valuePos < len(value) && (pattern[patternPos] == '?' || pattern[patternPos] == value[valuePos]):
			result = match(patternPos+1, valuePos+1)
		}
		memo[pos] = result
		return result
	}
	return match(0, 0)
}
