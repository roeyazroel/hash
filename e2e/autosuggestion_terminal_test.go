//go:build e2e

package e2e

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tfcace/hash/internal/autosuggest"
	"github.com/tfcace/hash/internal/editor"
)

type terminalAutosuggestStrategy struct {
	candidate string
}

func (terminalAutosuggestStrategy) Name() string { return "history" }

func (s terminalAutosuggestStrategy) Suggest(context.Context, autosuggest.Request) (string, error) {
	return s.candidate, nil
}

type observingTerminalWriter struct {
	mu       sync.Mutex
	output   strings.Builder
	needle   string
	seen     chan struct{}
	seenOnce sync.Once
}

func newObservingTerminalWriter(needle string) *observingTerminalWriter {
	return &observingTerminalWriter{needle: needle, seen: make(chan struct{})}
}

func (w *observingTerminalWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.output.Write(p)
	matched := strings.Contains(w.output.String(), w.needle)
	w.mu.Unlock()
	if matched {
		w.seenOnce.Do(func() { close(w.seen) })
	}
	return len(p), nil
}

func TestAutosuggestionTerminal_AcceptAndSubmit(t *testing.T) {
	tests := []struct {
		name string
		keys string
		want string
	}{
		{name: "right accepts full suggestion", keys: "\x1b[C\r", want: "git status"},
		{name: "enter submits typed buffer", keys: "\r", want: "git"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input, writeInput := io.Pipe()
			defer writeInput.Close()
			out := newObservingTerminalWriter(" status")
			engine := autosuggest.NewEngine(autosuggest.Config{}, []autosuggest.Strategy{
				terminalAutosuggestStrategy{candidate: "git status"},
			})
			ed := editor.New(editor.Config{
				Keybindings:           "emacs",
				AutosuggestionService: engine,
			}, input, out)

			resultCh := make(chan editor.Result, 1)
			errCh := make(chan error, 1)
			go func() {
				result, err := ed.Run(context.Background())
				resultCh <- result
				errCh <- err
			}()

			if _, err := io.WriteString(writeInput, "git"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-out.seen:
			case <-time.After(time.Second):
				t.Fatal("history ghost did not render before key acceptance")
			}
			if _, err := io.WriteString(writeInput, tt.keys); err != nil {
				t.Fatal(err)
			}
			if err := writeInput.Close(); err != nil {
				t.Fatal(err)
			}

			select {
			case err := <-errCh:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("editor did not finish")
			}
			if got := (<-resultCh).Text; got != tt.want {
				t.Errorf("submitted text = %q, want %q", got, tt.want)
			}
		})
	}
}
