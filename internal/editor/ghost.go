// internal/editor/ghost.go
package editor

import (
	"unicode"
	"unicode/utf8"
)

// GhostSource identifies the producer that owns visible ghost text.
type GhostSource uint8

const (
	GhostNone GhostSource = iota
	GhostHistory
	GhostPreviousCommand
	GhostCompletion
	GhostAgent
	GhostLearnedFix
)

// GhostText represents inline suggestion text that appears after the cursor.
// Ghost text is shown in dim gray and can be accepted with Tab or dismissed with Esc.
type GhostText struct {
	Text       string // The full ghost text suggestion
	AcceptedAt int    // Number of characters already accepted (for partial acceptance)
	Active     bool   // Whether ghost text is currently displayed
	Streaming  bool   // Whether more text is still arriving
	Source     GhostSource
	// FromAgent is kept for compatibility with existing callers. New code must
	// use Source and IsAgentOwned instead.
	FromAgent bool
}

// NewGhostText creates a new ghost text state.
func NewGhostText() *GhostText {
	return &GhostText{}
}

// Set sets the ghost text content.
func (g *GhostText) Set(text string) {
	g.SetSource(text, GhostHistory)
}

// SetSource sets ghost content with explicit ownership.
func (g *GhostText) SetSource(text string, source GhostSource) {
	g.Text = text
	g.AcceptedAt = 0
	g.Active = true
	g.Source = source
	g.FromAgent = source == GhostAgent
}

// Append adds more text to the ghost (for streaming).
func (g *GhostText) Append(text string) {
	g.Text += text
	g.Active = true
}

// Clear removes the ghost text.
func (g *GhostText) Clear() {
	g.Text = ""
	g.AcceptedAt = 0
	g.Active = false
	g.Streaming = false
	g.Source = GhostNone
	g.FromAgent = false
}

// IsAgentOwned reports whether this is interactive agent output, which keeps
// its dedicated hints and acceptance behavior.
func (g *GhostText) IsAgentOwned() bool {
	return g.Source == GhostAgent || g.FromAgent
}

// IsProtected reports ghost content that asynchronous autosuggestions may not
// overwrite before the user dismisses or edits it.
func (g *GhostText) IsProtected() bool {
	return g.IsAgentOwned() || g.Source == GhostLearnedFix || g.Source == GhostPreviousCommand
}

// Remaining returns the unaccepted portion of ghost text.
func (g *GhostText) Remaining() string {
	if g.AcceptedAt >= len(g.Text) {
		return ""
	}
	return g.Text[g.AcceptedAt:]
}

// AcceptAll accepts all remaining ghost text.
// Returns the text that should be inserted.
func (g *GhostText) AcceptAll() string {
	text := g.Remaining()
	g.Clear()
	return text
}

// AcceptWord accepts the next word of ghost text.
// Returns the text that should be inserted.
func (g *GhostText) AcceptWord() string {
	remaining := g.Remaining()
	if remaining == "" {
		g.Clear()
		return ""
	}

	// Consume leading whitespace, one complete Unicode word, and trailing
	// whitespace. All offsets remain byte offsets, but are advanced only by
	// decoded rune sizes so a partial acceptance never splits UTF-8.
	end := 0
	for end < len(remaining) {
		r, size := utf8.DecodeRuneInString(remaining[end:])
		if !unicode.IsSpace(r) {
			break
		}
		end += size
	}
	for end < len(remaining) {
		r, size := utf8.DecodeRuneInString(remaining[end:])
		if unicode.IsSpace(r) {
			break
		}
		end += size
	}
	for end < len(remaining) {
		r, size := utf8.DecodeRuneInString(remaining[end:])
		if !unicode.IsSpace(r) {
			break
		}
		end += size
	}

	accepted := remaining[:end]
	g.AcceptedAt += end

	// If we've accepted everything, clear
	if g.AcceptedAt >= len(g.Text) {
		g.Clear()
	}

	return accepted
}

// AcceptChar accepts the next character of ghost text.
// Returns the character that should be inserted.
func (g *GhostText) AcceptChar() string {
	remaining := g.Remaining()
	if remaining == "" {
		g.Clear()
		return ""
	}

	// Get first rune
	runes := []rune(remaining)
	r := runes[0]
	g.AcceptedAt += len(string(r))
	if g.AcceptedAt >= len(g.Text) {
		g.Clear()
	}
	return string(r)
}

// IsEmpty returns true if there's no ghost text.
func (g *GhostText) IsEmpty() bool {
	return g.Remaining() == ""
}

// SetStreaming marks the ghost text as still receiving data.
func (g *GhostText) SetStreaming(streaming bool) {
	g.Streaming = streaming
	if streaming {
		g.Active = true
	}
}
