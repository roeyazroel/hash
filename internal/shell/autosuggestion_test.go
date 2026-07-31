package shell

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tfcace/hash/internal/autosuggest"
	"github.com/tfcace/hash/internal/config"
	"github.com/tfcace/hash/internal/history"
)

func TestMakeEditorAutosuggestionService_UsesConfiguredHistoryStrategy(t *testing.T) {
	store, err := history.NewStore(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Add(history.Command{Command: "git status", ExitCode: 0, Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}

	cfg := config.Default().Autosuggestions
	service := makeEditorAutosuggestionService(cfg, store, nil, nil)
	got, err := service.Suggest(context.Background(), autosuggest.Request{Line: "git", Cursor: len("git")})
	if err != nil {
		t.Fatalf("Suggest() error = %v", err)
	}
	if got.Text != "git status" || got.Strategy != "history" {
		t.Errorf("Suggest() = %+v, want history candidate", got)
	}
}

func TestShell_AutosuggestionRequestUsesLastSuccessfulCommand(t *testing.T) {
	s := &Shell{lastCommand: "failed command", lastSuccessfulCommand: "git pull"}
	request := s.autosuggestionRequest("git", len("git"))
	if request.PreviousCommand != "git pull" {
		t.Errorf("PreviousCommand = %q, want git pull", request.PreviousCommand)
	}
}

func TestShell_AgentRequestDoesNotReplaceLastSuccessfulCommand(t *testing.T) {
	s := &Shell{lastSuccessfulCommand: "git status"}

	if err := s.dispatchCommand(context.Background(), "?? summarize this directory"); err != nil {
		t.Fatalf("dispatchCommand() error = %v", err)
	}
	if got := s.lastSuccessfulCommand; got != "git status" {
		t.Errorf("lastSuccessfulCommand = %q, want prior successful command", got)
	}
}

func TestNeedsPredictionStore_EnablesLearnedSequencesOnlyWhenRequested(t *testing.T) {
	cfg := config.Default()
	cfg.Prediction.Enabled = false
	cfg.Autosuggestions.Strategies = []string{"history"}
	if needsPredictionStore(cfg) {
		t.Fatal("history-only autosuggestions should not start prediction storage")
	}
	cfg.Autosuggestions.Strategies = []string{"match_prev_cmd"}
	if !needsPredictionStore(cfg) {
		t.Fatal("match_prev_cmd should start prediction storage even when prompt prediction is disabled")
	}
}
