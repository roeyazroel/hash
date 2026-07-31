// Package autosuggest contains UI-independent inline suggestion strategies.
package autosuggest

import "context"

// Request is the immutable editor state a strategy may inspect. Cursor is a
// byte offset because it maps directly to Go string slices in the editor.
type Request struct {
	Line            string
	Cursor          int
	PreviousCommand string
	CWD             string
}

// Candidate is a full replacement line. The engine accepts it only when it is
// a strict extension of Request.Line, so callers can render just its suffix.
type Candidate struct {
	Text     string
	Strategy string
}

// Strategy returns a possible full-line candidate or an empty string on a
// miss. Implementations must respect ctx and must not mutate editor state.
type Strategy interface {
	Name() string
	Suggest(context.Context, Request) (string, error)
}

// Config controls engine-level eligibility. Strategy-specific settings belong
// to the strategy adapters, keeping the engine independent from config/TOML.
type Config struct {
	MinInputLength int
	MaxBufferSize  int
	OnError        func(Strategy, error)
}
