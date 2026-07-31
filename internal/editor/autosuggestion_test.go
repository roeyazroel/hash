package editor

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/tfcace/hash/internal/autosuggest"
)

type autosuggestionCall struct {
	ctx      context.Context
	request  autosuggest.Request
	response chan autosuggest.Candidate
}

type controlledAutosuggestionService struct {
	calls chan autosuggestionCall
}

func newControlledAutosuggestionService() *controlledAutosuggestionService {
	return &controlledAutosuggestionService{calls: make(chan autosuggestionCall, 8)}
}

func (s *controlledAutosuggestionService) Suggest(ctx context.Context, request autosuggest.Request) (autosuggest.Candidate, error) {
	call := autosuggestionCall{ctx: ctx, request: request, response: make(chan autosuggest.Candidate, 1)}
	s.calls <- call
	return <-call.response, nil
}

func newAutosuggestionEditor(service AutosuggestionService, text string) *Editor {
	ed := New(Config{Keybindings: "emacs", AutosuggestionService: service}, strings.NewReader(""), io.Discard)
	ed.state.Buffer = NewBufferFromString(text)
	ed.state.Cursor.MoveTo(0, len(text))
	return ed
}

func TestEditor_AutosuggestionNewestRequestWinsWithoutBlockingInput(t *testing.T) {
	service := newControlledAutosuggestionService()
	ed := newAutosuggestionEditor(service, "gi")
	ed.updateSuggestion()
	first := <-service.calls

	if _, done := ed.handleKeyEvent(Key{Rune: 't'}); done {
		t.Fatal("typing must not end the editor")
	}
	select {
	case <-first.ctx.Done():
	default:
		t.Fatal("typing should cancel the obsolete request")
	}
	second := <-service.calls
	if second.request.Line != "git" {
		t.Fatalf("second request line = %q, want git", second.request.Line)
	}

	first.response <- autosuggest.Candidate{Text: "git stale", Strategy: "history"}
	ed.handleAutosuggestionResult(<-ed.autosuggestionResults)
	if ed.ghost.Active {
		t.Fatal("late stale result became visible")
	}

	second.response <- autosuggest.Candidate{Text: "git status", Strategy: "history"}
	ed.handleAutosuggestionResult(<-ed.autosuggestionResults)
	if got := ed.ghost.Remaining(); got != " status" {
		t.Errorf("ghost = %q, want suffix ' status'", got)
	}
}

func TestEditor_AutosuggestionTypesThroughVisibleCandidateWithoutLookup(t *testing.T) {
	service := newControlledAutosuggestionService()
	ed := newAutosuggestionEditor(service, "gi")
	ed.updateSuggestion()
	call := <-service.calls
	call.response <- autosuggest.Candidate{Text: "git status", Strategy: "history"}
	ed.handleAutosuggestionResult(<-ed.autosuggestionResults)

	ed.handleKeyEvent(Key{Rune: 't'})
	if got := ed.ghost.Remaining(); got != " status" {
		t.Errorf("ghost after typing through = %q, want ' status'", got)
	}
	select {
	case extra := <-service.calls:
		t.Fatalf("typed-through candidate started an unnecessary request for %+v", extra.request)
	default:
	}
}

func TestEditor_AutosuggestionLifecycleEventsCancelPendingLookup(t *testing.T) {
	tests := []struct {
		name string
		act  func(*Editor)
	}{
		{name: "move left", act: func(ed *Editor) { ed.handleKeyEvent(Key{Special: KeyLeft}) }},
		{name: "tab", act: func(ed *Editor) { ed.handleKeyEvent(Key{Special: KeyTab}) }},
		{name: "ctrl c", act: func(ed *Editor) { ed.handleKeyEvent(Key{Ctrl: true, Rune: 'c'}) }},
		{name: "agent streaming", act: func(ed *Editor) { ed.SetGhostTextStreaming(make(chan string), make(chan error)) }},
		{name: "close", act: func(ed *Editor) { ed.Close() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newControlledAutosuggestionService()
			ed := newAutosuggestionEditor(service, "git")
			ed.updateSuggestion()
			call := <-service.calls

			tt.act(ed)
			select {
			case <-call.ctx.Done():
			default:
				t.Fatal("pending lookup was not canceled")
			}
			call.response <- autosuggest.Candidate{}
			if tt.name != "close" {
				<-ed.autosuggestionResults
			}
		})
	}
}
