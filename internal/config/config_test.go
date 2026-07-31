package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func TestAutosuggestionsConfig_Defaults(t *testing.T) {
	cfg := Default().Autosuggestions

	if !cfg.Enabled {
		t.Error("autosuggestions should be enabled by default")
	}
	if !reflect.DeepEqual(cfg.Strategies, []string{"history"}) {
		t.Errorf("Strategies = %v, want [history]", cfg.Strategies)
	}
	if cfg.MinInputLength != 2 {
		t.Errorf("MinInputLength = %d, want 2", cfg.MinInputLength)
	}
	if cfg.MaxBufferSize != 0 {
		t.Errorf("MaxBufferSize = %d, want 0", cfg.MaxBufferSize)
	}
	if cfg.CompletionTimeout != "150ms" {
		t.Errorf("CompletionTimeout = %q, want 150ms", cfg.CompletionTimeout)
	}
	if got, err := cfg.ParseCompletionTimeout(); err != nil || got != 150*time.Millisecond {
		t.Errorf("ParseCompletionTimeout() = %v, %v; want 150ms, nil", got, err)
	}
}

func TestAutosuggestionsConfig_LoadPreservesStrategyOrder(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte(`
[autosuggestions]
enabled = false
strategies = ["completion", "match_prev_cmd", "history"]
min_input_length = 4
max_buffer_size = 100
history_ignore = ["secret*"]
completion_ignore = ["git push*"]
completion_timeout = "250ms"
`)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.toml"), content, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := cfg.Autosuggestions
	if got.Enabled || !reflect.DeepEqual(got.Strategies, []string{"completion", "match_prev_cmd", "history"}) ||
		got.MinInputLength != 4 || got.MaxBufferSize != 100 ||
		!reflect.DeepEqual(got.HistoryIgnore, []string{"secret*"}) ||
		!reflect.DeepEqual(got.CompletionIgnore, []string{"git push*"}) || got.CompletionTimeout != "250ms" {
		t.Errorf("Autosuggestions = %+v, want configured values", got)
	}
}

func TestAutosuggestionsConfig_Validation(t *testing.T) {
	tests := []struct {
		name string
		cfg  AutosuggestionsConfig
	}{
		{name: "empty strategy", cfg: AutosuggestionsConfig{Strategies: []string{""}, CompletionTimeout: "1ms"}},
		{name: "unknown strategy", cfg: AutosuggestionsConfig{Strategies: []string{"remote"}, CompletionTimeout: "1ms"}},
		{name: "duplicate strategy", cfg: AutosuggestionsConfig{Strategies: []string{"history", "history"}, CompletionTimeout: "1ms"}},
		{name: "negative minimum", cfg: AutosuggestionsConfig{Strategies: []string{"history"}, MinInputLength: -1, CompletionTimeout: "1ms"}},
		{name: "negative maximum", cfg: AutosuggestionsConfig{Strategies: []string{"history"}, MaxBufferSize: -1, CompletionTimeout: "1ms"}},
		{name: "zero timeout", cfg: AutosuggestionsConfig{Strategies: []string{"history"}, CompletionTimeout: "0s"}},
		{name: "negative timeout", cfg: AutosuggestionsConfig{Strategies: []string{"history"}, CompletionTimeout: "-1s"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want error")
			}
		})
	}
}

func TestAutosuggestionsConfig_LoadRejectsInvalidConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte(`
[autosuggestions]
strategies = ["history", "history"]
`)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.toml"), content, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	_, err := Load(tmpDir)
	if err == nil || !strings.Contains(err.Error(), "autosuggestions") {
		t.Fatalf("Load() error = %v, want autosuggestions validation error", err)
	}
}

func TestAutosuggestionsConfig_RecoveryResetsInvalidAutosuggestions(t *testing.T) {
	tmpDir := t.TempDir()
	content := []byte(`
[prompt]
mode = 42

[autosuggestions]
strategies = ["remote"]
completion_timeout = "0s"
`)
	if err := os.WriteFile(filepath.Join(tmpDir, "config.toml"), content, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)
	if err == nil {
		t.Fatal("Load() error = nil, want recovered config load issue")
	}
	if !reflect.DeepEqual(cfg.Autosuggestions, Default().Autosuggestions) {
		t.Errorf("Autosuggestions = %+v, want defaults after invalid recovered section", cfg.Autosuggestions)
	}

	found := false
	for _, section := range cfg.LoadIssue.BadSections {
		if section == "autosuggestions" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("BadSections = %v, want autosuggestions", cfg.LoadIssue.BadSections)
	}
}

func TestAutosuggestionsConfig_TOMLRoundTrip(t *testing.T) {
	want := Default().Autosuggestions
	data, err := toml.Marshal(struct {
		Autosuggestions AutosuggestionsConfig `toml:"autosuggestions"`
	}{Autosuggestions: want})
	if err != nil {
		t.Fatalf("toml.Marshal() error = %v", err)
	}
	var got struct {
		Autosuggestions AutosuggestionsConfig `toml:"autosuggestions"`
	}
	if err := toml.Unmarshal(data, &got); err != nil {
		t.Fatalf("toml.Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(got.Autosuggestions, want) {
		t.Errorf("round trip = %+v, want %+v", got.Autosuggestions, want)
	}
}

func TestLoadConfig_DefaultValues(t *testing.T) {
	// Create temp dir with no config file
	tmpDir := t.TempDir()

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Check defaults
	if cfg.Shell.Keybindings != "emacs" {
		t.Errorf("Keybindings = %q, want %q", cfg.Shell.Keybindings, "emacs")
	}
	if cfg.Shell.Dialect != "bash" {
		t.Errorf("Dialect = %q, want %q", cfg.Shell.Dialect, "bash")
	}
	if cfg.Prompt.Mode != "starship" {
		t.Errorf("Prompt.Mode = %q, want %q", cfg.Prompt.Mode, "starship")
	}
}

func TestLoadConfig_FromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := []byte(`
[shell]
keybindings = "vim"
editor = "nvim"
dialect = "zsh"

[prompt]
mode = "built-in"
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Shell.Keybindings != "vim" {
		t.Errorf("Keybindings = %q, want %q", cfg.Shell.Keybindings, "vim")
	}
	if cfg.Shell.Editor != "nvim" {
		t.Errorf("Editor = %q, want %q", cfg.Shell.Editor, "nvim")
	}
	if cfg.Shell.Dialect != "zsh" {
		t.Errorf("Dialect = %q, want %q", cfg.Shell.Dialect, "zsh")
	}
	if cfg.Prompt.Mode != "built-in" {
		t.Errorf("Prompt.Mode = %q, want %q", cfg.Prompt.Mode, "built-in")
	}
}

func TestLoadConfig_NamedAgents(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := []byte(`
[agent]
default = "ollama"
timeout = "45s"

[agent.ollama]
transport = "http"
url = "http://localhost:11434/api/generate"
model = "codellama:13b"
headers = { Authorization = "Bearer token" }
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	agent := cfg.EffectiveAgent()
	if agent.Transport != "http" {
		t.Errorf("Transport = %q, want http", agent.Transport)
	}
	if agent.URL != "http://localhost:11434/api/generate" {
		t.Errorf("URL = %q", agent.URL)
	}
	if agent.Model != "codellama:13b" {
		t.Errorf("Model = %q", agent.Model)
	}
	if agent.Timeout != "45s" {
		t.Errorf("Timeout = %q, want 45s", agent.Timeout)
	}
	if agent.Headers["Authorization"] != "Bearer token" {
		t.Errorf("Authorization header = %q", agent.Headers["Authorization"])
	}
}

func TestEffectiveAgent_FlatConfigStillWorks(t *testing.T) {
	cfg := Default()
	cfg.Agent.Default = "custom"
	cfg.Agent.Transport = "http"
	cfg.Agent.URL = "http://localhost:11434/api/generate"
	cfg.Agent.Model = "llama3"

	agent := cfg.EffectiveAgent()
	if agent.Transport != "http" || agent.URL == "" || agent.Model != "llama3" {
		t.Fatalf("flat agent config not preserved: %+v", agent)
	}
}

func TestConfig_StartupFiles(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := `
[shell]
profile = [
    "export FOO=bar",
]
rc_commands = [
    "alias ll='ls -la'",
]

[shell.startup_files]
login = [
    "/etc/profile",
    "~/.profile",
    "~/.hash_profile",
]
interactive = [
    "~/.hashrc",
]
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	// Check profile commands
	if len(cfg.Shell.ProfileCommands) != 1 {
		t.Errorf("expected 1 profile command, got %d", len(cfg.Shell.ProfileCommands))
	}
	if len(cfg.Shell.RCCommands) != 1 {
		t.Errorf("expected 1 rc command, got %d", len(cfg.Shell.RCCommands))
	}

	// Check startup files
	if len(cfg.Shell.StartupFiles.Login) != 3 {
		t.Errorf("expected 3 login files, got %d", len(cfg.Shell.StartupFiles.Login))
	}
	if len(cfg.Shell.StartupFiles.Interactive) != 1 {
		t.Errorf("expected 1 interactive file, got %d", len(cfg.Shell.StartupFiles.Interactive))
	}
}

func TestConfig_StartupFilesDefaults(t *testing.T) {
	cfg := Default()

	// Check default startup files
	if len(cfg.Shell.StartupFiles.Login) == 0 {
		t.Error("expected default login startup files")
	}
	if len(cfg.Shell.StartupFiles.Interactive) == 0 {
		t.Error("expected default interactive startup files")
	}
}

func TestConfig_ClipboardMaxOutputSize(t *testing.T) {
	cfg := Default()

	// Default should be 1MB
	size, err := cfg.Clipboard.ParseMaxOutputSize()
	if err != nil {
		t.Fatalf("ParseMaxOutputSize error: %v", err)
	}
	if size != 1024*1024 {
		t.Errorf("Default size = %d, want %d", size, 1024*1024)
	}
}

func TestConfig_ClipboardMaxOutputSizeParsing(t *testing.T) {
	tests := []struct {
		input string
		want  int64
	}{
		{"1MB", 1024 * 1024},
		{"5MB", 5 * 1024 * 1024},
		{"500KB", 500 * 1024},
		{"1024", 1024},
	}

	for _, tt := range tests {
		cfg := &ClipboardConfig{MaxOutputSize: tt.input}
		got, err := cfg.ParseMaxOutputSize()
		if err != nil {
			t.Errorf("ParseMaxOutputSize(%q) error: %v", tt.input, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseMaxOutputSize(%q) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestLoadConfig_ParseError_ReturnsDefaultsWithError(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write invalid TOML
	invalidContent := []byte(`
[shell
keybindings = "vim"
`)
	if err := os.WriteFile(configPath, invalidContent, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)

	// Should return an error
	if err == nil {
		t.Error("Load() should return error for invalid TOML")
	}

	// But should also return usable defaults
	if cfg == nil {
		t.Fatal("Load() should return defaults even on parse error")
		return
	}

	// Check that defaults are applied
	if cfg.Shell.Keybindings != "emacs" {
		t.Errorf("Keybindings = %q, want default %q", cfg.Shell.Keybindings, "emacs")
	}
	if cfg.Prompt.Mode != "starship" {
		t.Errorf("Prompt.Mode = %q, want default %q", cfg.Prompt.Mode, "starship")
	}
}

func TestLoadWithWarnings_WritesWarning(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write invalid TOML
	invalidContent := []byte(`invalid toml [[[`)
	if err := os.WriteFile(configPath, invalidContent, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	var buf bytes.Buffer
	cfg := LoadWithWarnings(tmpDir, &buf)

	// Should return usable config
	if cfg == nil {
		t.Fatal("LoadWithWarnings() should return config")
	}

	// Should have written a warning
	warning := buf.String()
	if warning == "" {
		t.Error("LoadWithWarnings() should write warning for invalid config")
	}
	if !strings.Contains(warning, "Warning") {
		t.Errorf("Warning should contain 'Warning', got: %q", warning)
	}
}

func TestLoadConfig_HooksChpwd(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	content := []byte(`
[shell.hooks]
chpwd = ["zoxide add -- \"$PWD\"", "echo changed"]
`)
	if err := os.WriteFile(configPath, content, 0o644); err != nil { //nolint:gosec // G306: test file
		t.Fatal(err)
	}

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if len(cfg.Shell.Hooks.Chpwd) != 2 {
		t.Fatalf("expected 2 chpwd hooks, got %d", len(cfg.Shell.Hooks.Chpwd))
	}
	if cfg.Shell.Hooks.Chpwd[0] != `zoxide add -- "$PWD"` {
		t.Errorf("Hooks.Chpwd[0] = %q, want %q", cfg.Shell.Hooks.Chpwd[0], `zoxide add -- "$PWD"`)
	}
	if cfg.Shell.Hooks.Chpwd[1] != "echo changed" {
		t.Errorf("Hooks.Chpwd[1] = %q, want %q", cfg.Shell.Hooks.Chpwd[1], "echo changed")
	}
}

func TestLoadConfig_HooksChpwd_Default(t *testing.T) {
	cfg := Default()

	// Default should have no chpwd hooks
	if len(cfg.Shell.Hooks.Chpwd) != 0 {
		t.Errorf("expected 0 default chpwd hooks, got %d", len(cfg.Shell.Hooks.Chpwd))
	}
}
