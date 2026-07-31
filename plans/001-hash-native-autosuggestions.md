# Implement Hash-native autosuggestions

Status: proposed  
Priority: P1  
Effort: L  
Risk: high  
Category: editor / completion / prediction

## Why

Hash already displays a history-derived ghost suffix, but the implementation is a synchronous callback with one fixed strategy. The useful behavior in `zsh-users/zsh-autosuggestions` is broader:

- suggestions are editor middleware, refreshed after editing actions;
- an ordered list of strategies returns the first valid prefix candidate;
- history, previous-command context, and completion can all supply candidates;
- slow work is asynchronous and stale work is canceled or ignored;
- the rendered suggestion is not inserted into the editable buffer until an accept action;
- full and partial acceptance are distinct operations.

The goal is behavioral parity in Hash's native editor, not compatibility with ZLE variables, widgets, or shell plugin APIs.

## Planning snapshot and drift check

- Plan written: 2026-07-31.
- Hash baseline inspected: `a22cf36864190970ec53be08f9698295fb779fbe` (`0.7.2`).
- Upstream behavior inspected at `zsh-users/zsh-autosuggestions@85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5`.
- Focused baseline passed before planning:

  ```sh
  rtk go test ./internal/editor/... ./internal/history/... ./internal/prediction/... ./internal/completion/... ./internal/config/... ./internal/shell/...
  ```

  Observed result: 930 tests passed across 7 packages.

- The planning worktree was dirty, including user changes in `internal/editor/editor.go`, `internal/editor/display.go`, and `internal/shell/shell.go`. Those files overlap this plan. The implementer must reconcile the plan against those changes and must not overwrite or discard them.

Before implementation, run:

```sh
rtk git rev-parse HEAD
rtk git status --short
rtk git diff -- internal/editor/editor.go internal/editor/display.go internal/shell/shell.go
rtk git log --oneline a22cf36864190970ec53be08f9698295fb779fbe..HEAD -- internal/editor internal/history internal/prediction internal/completion internal/config internal/shell
```

If `HEAD` has moved, re-read every symbol named in this plan and update the file list and tests before editing. If overlapping uncommitted changes are still present, stop until their owner has either committed them or explicitly described how the autosuggestion work should be layered onto them.

## Reverse-engineered behavior

The upstream implementation is small because it composes with ZLE rather than owning a terminal editor. Its relevant mechanics map to Hash as follows:

| Upstream mechanism | Behavior to preserve | Hash-native equivalent |
|---|---|---|
| Wrap nearly every ZLE widget | Refresh or clear after an editing action while preserving the original action | Trigger from Hash's editor result/action path after buffer or cursor changes |
| `BUFFER` plus `POSTDISPLAY` | Keep the suggestion outside editable text until acceptance | `Buffer` plus `GhostText` |
| Ordered strategy functions | Stop at the first non-empty candidate that starts with the input | An ordered `autosuggest.Engine` with typed strategies |
| Fork plus `zle -F` callback | Never block input; cancel the previous lookup; only apply the newest result | `context.CancelFunc`, a result channel, and monotonically increasing request IDs |
| `history` strategy | Most recent literal prefix match | Existing SQLite history prefix query |
| `match_prev_cmd` strategy | Prefer a prefix match associated with the previously executed command | Existing `command_sequences` data exposed through a prefix-aware query |
| `completion` strategy through `zpty` | Reuse completion while ensuring the result remains a literal extension of the buffer | Restricted, prefix-only invocation of Hash's completion router |
| Accept/partial-accept widgets | Insert the whole suffix or only the next word | Explicit editor actions and UTF-8-safe ghost slicing |
| Widget rebinding on `precmd` | Survive other ZLE plugins replacing widgets | No analogue; Hash owns its editor dispatch |
| ZLE highlight style string | Muted visual suggestion | Hash palette/config, never raw terminal escape sequences |

Important upstream references:

- Repository and user-facing semantics: <https://github.com/zsh-users/zsh-autosuggestions/tree/85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5>
- Widget interception: <https://github.com/zsh-users/zsh-autosuggestions/blob/85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5/src/bind.zsh>
- Modify, accept, execute, and partial-accept behavior: <https://github.com/zsh-users/zsh-autosuggestions/blob/85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5/src/widgets.zsh>
- Ordered fetching: <https://github.com/zsh-users/zsh-autosuggestions/blob/85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5/src/fetch.zsh>
- Newest-request-wins asynchronous flow: <https://github.com/zsh-users/zsh-autosuggestions/blob/85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5/src/async.zsh>
- Strategies: <https://github.com/zsh-users/zsh-autosuggestions/tree/85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5/src/strategies>

## Current Hash state

At the inspected commit:

- `internal/editor/editor.go:42` exposes `SuggestionFunc func(input string) string`.
- `internal/editor/editor.go:605` calls that function synchronously from `updateSuggestion`, only when the cursor is at the end and the input is at least two characters.
- `internal/shell/shell.go:321` wires the callback; `internal/shell/shell.go:1676` implements it as a history lookup. The predictor parameter is reserved but does not provide a fallback.
- `internal/history/store.go:264` already returns recent, successful, deduplicated literal-prefix matches.
- `internal/prediction/predictor.go:31` predicts a next command from the previous executable and working directory, backed by `command_sequences`; it does not accept a typed prefix.
- `internal/completion/router.go:83` provides ordered completion and `router.go:225` provides bounded execution.
- `internal/completion/types.go:49` defines `PriorityAgent`. Speculative autosuggestions must never cross that boundary.
- `internal/editor/editor.go:689` contains special ghost key handling. Right accepts all, a modified Tab accepts one word, plain Tab opens interactive completion for history ghosts, Escape clears, and Enter does not execute a history suggestion.
- `internal/editor/ghost.go:59` performs partial acceptance. Its rune loop and byte slicing need dedicated non-ASCII tests before reuse.
- `internal/editor/display.go:431` renders ghost text but currently truncates it at the first newline.
- Empty-prompt learned-fix and next-command prediction in `internal/shell/fix_tracker.go` are separate features. This work must not change their timing or precedence.

## Product decisions

Implement a first-class `[autosuggestions]` feature, separate from `[prediction]`:

```toml
[autosuggestions]
enabled = true
strategies = ["history"]
min_input_length = 2
max_buffer_size = 0
history_ignore = []
completion_ignore = []
completion_timeout = "150ms"
```

Rules:

1. `strategies` is ordered and accepts `history`, `match_prev_cmd`, and `completion`.
2. Keep `history` as the only default strategy so the upgrade does not start filesystem, tool, or network work on every keystroke.
3. `completion` is opt-in, prefix-only, bounded, and excludes agent completion unconditionally.
4. `max_buffer_size = 0` means unlimited. Size is measured in runes, not bytes.
5. Ignore patterns use Hash's documented simple wildcard semantics, not zsh extended glob syntax.
6. A candidate is valid only when it is a strict extension of the exact current buffer and the cursor is at the end. Never display an edit, replacement, fuzzy match, or duplicate of the buffer as a ghost suffix.
7. Agent/learned-fix ghost text keeps precedence. Inline autosuggestion results may not overwrite it.
8. Right Arrow and End accept the full candidate. The existing modified-Tab behavior continues to accept one word. Plain Tab remains interactive completion and takes priority over speculative completion.
9. Enter submits only the user's current buffer. This avoids executing unseen text by accident.
10. Multi-line candidates are supported and rendered as ghost text; they remain outside the buffer until accepted.

## Scope

### In scope

- Ordered `history`, `match_prev_cmd`, and `completion` strategies.
- Context cancellation, request generations, and stale-result rejection.
- Reuse of the visible suffix while the user types through an existing candidate.
- Config validation, defaults, and documentation.
- Whole and partial acceptance, including UTF-8 and multi-line cases.
- Strict separation between interactive Tab completion and speculative completion.
- Unit, integration, race, and focused PTY coverage.

### Out of scope

- Sourcing the zsh plugin or emulating ZLE widget/global-variable APIs.
- Arbitrary user-defined strategy functions.
- Configurable widget arrays or a general keybinding framework.
- Runtime enable/disable/toggle/fetch commands.
- `autosuggest-execute`; Enter continues to submit only visible buffer content.
- Calls to Hash's agent completer or any network/paid provider while typing.
- Exact zsh extended-glob compatibility.
- Importing unexecuted or failed shell history into the existing successful-command policy.
- Raw ANSI style configuration. Use the existing Hash palette and theme abstractions.

## Proposed architecture

Create `internal/autosuggest` as a UI-independent orchestration package:

```go
type Request struct {
    Line            string
    Cursor          int
    PreviousCommand string
    CWD             string
}

type Candidate struct {
    Text     string
    Strategy string
}

type Strategy interface {
    Name() string
    Suggest(context.Context, Request) (string, error)
}
```

`Engine.Suggest` iterates the configured strategies and returns the first strict-prefix extension. `context.Canceled` exits immediately. A strategy miss continues. Operational errors are observable through debug logging but do not prevent later strategies from running unless the context is canceled.

The editor owns concurrency because only the editor can validate freshness:

- increment a request ID and cancel the prior request whenever eligible buffer state changes;
- run `Engine.Suggest` away from the input loop;
- send `{requestID, line, cursor, candidate}` to a buffered editor channel;
- apply only when the ID, line, cursor, prompt generation, and ghost ownership still match;
- cancel on submit, Ctrl-C, editor close, prompt reset, interactive Tab, or agent streaming;
- when the newly typed buffer is still a prefix of the already known full candidate, slice the remaining suffix locally and avoid another lookup.

Do not allow worker goroutines to mutate `Editor`, `Buffer`, `Cursor`, or `GhostText` directly.

Use a typed ghost source instead of growing `FromAgent bool` into more booleans:

```go
type GhostSource uint8

const (
    GhostNone GhostSource = iota
    GhostHistory
    GhostPreviousCommand
    GhostCompletion
    GhostAgent
    GhostLearnedFix
)
```

Keep helper predicates such as `IsAgentOwned()` so existing behavior stays explicit during the migration.

## File plan

Create:

- `internal/autosuggest/types.go`
- `internal/autosuggest/engine.go`
- `internal/autosuggest/engine_test.go`
- `internal/autosuggest/history.go`
- `internal/autosuggest/history_test.go`
- `internal/autosuggest/match_prev_cmd.go`
- `internal/autosuggest/match_prev_cmd_test.go`
- `internal/autosuggest/completion.go`
- `internal/autosuggest/completion_test.go`
- `internal/editor/autosuggestion_test.go`
- `e2e/autosuggestion_terminal_test.go`

Modify as needed after drift review:

- `internal/config/config.go`
- `internal/config/config_test.go`
- `docs/config-reference.md`
- `internal/history/store.go`
- `internal/history/store_test.go`
- `internal/prediction/predictor.go`
- `internal/prediction/predictor_test.go`
- `internal/prediction/store.go`
- `internal/prediction/store_test.go`
- `internal/completion/types.go`
- `internal/completion/router.go`
- `internal/completion/router_test.go`
- `internal/shell/completion_feedback.go`
- `internal/shell/completion_feedback_test.go`
- `internal/editor/editor.go`
- `internal/editor/ghost.go`
- `internal/editor/ghost_test.go`
- `internal/editor/display.go`
- `internal/editor/display_test.go`
- `internal/editor/insert.go`
- `internal/editor/integration_test.go`
- `internal/shell/shell.go`
- `internal/shell/shell_test.go`
- `e2e/README.md`

Delete nothing.

## Implementation sequence

Every numbered step is test-first. Add the named focused test, run it and confirm it fails for the expected missing behavior, make the smallest production change, rerun the same command to green, then run the next broader validation slice.

### 1. Add and validate configuration

Add failing table tests for defaults, a valid three-strategy order, unknown/duplicate strategy names, negative sizes/timeouts, and TOML round trips. Then add `AutosuggestionsConfig` to the root config and document every field.

Validation behavior:

- default: enabled, `history`, minimum length 2, unlimited max size, 150 ms completion timeout;
- reject an empty strategy name, unknown names, duplicates, negative sizes, and non-positive timeout;
- preserve user order;
- ignore lists default to empty.

Commands:

```sh
rtk go test ./internal/config -run Autosuggestion -count=1
rtk go test ./internal/config/... -count=1
```

### 2. Build the ordered engine as a pure package

Add fake-strategy tests proving:

- strategies run in configuration order and stop at the first valid candidate;
- empty results and ordinary errors fall through;
- cancellation stops traversal;
- candidates equal to the input, not starting with the input, or returned for a non-terminal cursor are rejected;
- minimum and maximum sizes use rune counts;
- ignore matching is deterministic and independent of zsh.

Implement only after the first test run fails.

Commands:

```sh
rtk go test ./internal/autosuggest -run 'Engine|Config|Ignore' -count=1
rtk go test ./internal/autosuggest/... -count=1
```

### 3. Implement history and previous-command strategies

First add tests with provider fakes, then storage-level tests.

History must continue to return the most recent successful, deduplicated, literal-prefix match and must honor `history_ignore` after retrieval. Convert the database query to a context-aware variant so canceled requests stop promptly; keep the old method as a compatibility wrapper only if other callers require it.

For `match_prev_cmd`, add a prefix-aware predictor/store API rather than reading history rows from the editor. It should query learned `command_sequences` using the normalized previous executable, typed prefix, and current working directory. Rank exact-CWD matches before global/fallback matches, then use the existing count/recency scoring. Return only a strict extension. Document this as Hash's learned-sequence equivalent of upstream adjacent-history matching; it intentionally retains Hash's successful-command data policy.

Required cases:

- no previous command;
- previous command differs;
- exact-CWD and fallback ordering;
- special characters in a literal prefix;
- canceled database context;
- ignored result;
- history wins when it appears earlier in the configured order.

Commands:

```sh
rtk go test ./internal/history ./internal/prediction ./internal/autosuggest -run 'Prefix|History|Previous|MatchPrev|Cancel' -count=1
rtk go test ./internal/history/... ./internal/prediction/... ./internal/autosuggest/... -count=1
```

### 4. Add a safe speculative-completion lane

Write router and adapter tests before changing completion code. Add explicit request options or a separate router method; do not smuggle behavior through context values.

The speculative call must:

- include only local completers with priority lower than `PriorityAgent`;
- disable fuzzy matching and accept only a candidate that reconstructs to a strict extension of the full line;
- respect the configured timeout and caller cancellation;
- return at most the first candidate needed by the strategy;
- yield immediately when interactive Tab completion begins;
- never share an in-flight coalescing key that causes interactive completion to wait for speculative work.

Extract one pure helper that reconstructs the full line from the completion result's replacement prefix/item and use it in both interactive completion acceptance and autosuggestion validation. Cover quoted tokens, path separators, cursor-at-end, and a completer returning a replacement rather than an extension.

Instrument a fake agent completer that fails the test if called. Add a blocking local completer to prove Tab preemption and goroutine cleanup.

Commands:

```sh
rtk go test ./internal/completion ./internal/autosuggest ./internal/shell -run 'Speculative|Autosuggest|Reconstruct|Preempt|Agent' -count=1
rtk go test -race ./internal/completion/... ./internal/autosuggest/... -count=1
```

### 5. Make editor lookup asynchronous and freshness-safe

Add deterministic editor tests with a controllable fake engine. Do not use sleeps; block and release fake requests through channels.

Test this sequence before implementation:

1. Type `g`, then `gi`; the editor input loop remains responsive.
2. Request A for `gi` blocks.
3. Type `t`; A is canceled and request B is issued for `git`.
4. A returns late; it is ignored.
5. B returns `git status`; only ` status` is rendered.
6. Moving left, pressing Tab, submitting, Ctrl-C, starting agent output, or closing the editor cancels/invalidates B.

Also test the upstream optimization: given candidate `git status`, typing the `t` from a displayed suffix should locally reduce the suffix without querying again while the full candidate remains valid.

Replace the synchronous callback with a context-aware service/interface. Keep all editor state mutation in the event loop. Buffer the result channel so a canceled worker can exit without waiting on a receiver. Ensure close is idempotent and leaves no goroutine blocked.

Commands:

```sh
rtk go test ./internal/editor -run 'Autosuggestion|Stale|Cancel|TypeThrough|Close' -count=1
rtk go test -race ./internal/editor/... -count=1
```

### 6. Preserve and extend acceptance semantics

Add key-level tests before editing handlers:

- Right Arrow and End accept the whole suffix only at end-of-buffer;
- the existing word-accept binding accepts through the next word boundary;
- word acceptance is correct for ASCII, punctuation, emoji, combining input, and multi-byte UTF-8;
- plain Tab cancels speculative work, clears the autosuggestion, and opens interactive completion;
- Escape clears without editing the buffer;
- Enter submits only the typed buffer;
- agent ghost behavior is unchanged;
- editing in the middle clears/cancels autosuggestions.

Fix `GhostText.AcceptWord` to use rune-safe boundaries and translate ghost ownership to `GhostSource`. Route End through the same full-accept action as Right. Do not introduce configurable accept-key arrays in this change.

Commands:

```sh
rtk go test ./internal/editor -run 'Ghost|Accept|Tab|End|UTF|Agent' -count=1
rtk go test ./internal/editor/... -count=1
```

### 7. Render multi-line suggestions correctly

Add display tests that snapshot logical output for one-line, wrapped, and multi-line ghost suffixes at narrow and wide terminal widths. Include a candidate whose newline follows the typed prefix and ensure cursor restoration remains on the editable buffer, not at the visual end of the ghost.

Remove first-line truncation only after those tests fail. Reuse the existing layout/wrapping primitives and existing muted palette color. Do not write raw ANSI sequences from config.

Commands:

```sh
rtk go test ./internal/editor -run 'Render.*Ghost|Multiline|Wrap' -count=1
rtk go test ./internal/editor/... -count=1
```

### 8. Wire the shell without changing prompt prediction

Add shell-level tests for strategy construction and request metadata before replacing `makeEditorSuggestionFunc`.

Wire:

- history and predictor providers;
- the restricted completion adapter;
- the current working directory;
- the last successfully executed command;
- configured strategy order and limits.

Keep `promptGhost()` behavior in `internal/shell/fix_tracker.go` unchanged. A learned-fix or empty-prompt prediction must retain ownership until the user starts editing or dismisses it. Verify builtins, external commands, failed commands, and agent-executed commands against the existing definition of “previous command”; do not silently broaden prediction recording.

Commands:

```sh
rtk go test ./internal/shell -run 'Autosuggestion|Suggestion|PromptGhost|PreviousCommand' -count=1
rtk go test ./internal/shell/... ./internal/editor/... ./internal/autosuggest/... -count=1
```

### 9. Add terminal-level regression coverage

Follow the existing PTY transcript helpers in `e2e/acp_terminal_ux_test.go`. Add a deterministic fixture/history setup and verify:

- a history suggestion appears without altering the editable buffer;
- Right and End accept it;
- word acceptance inserts only part;
- Enter without acceptance executes only typed text;
- fast typing never flashes or accepts a stale candidate;
- plain Tab still opens/accepts interactive completion when completion strategy work is pending;
- multi-line ghost rendering does not corrupt the next prompt.

Avoid timing assertions. Use observable prompts/channels and bounded polling already established by the suite.

Commands:

```sh
rtk go test -tags=e2e ./e2e/... -run AutosuggestionTerminal -count=1
rtk go test -tags=e2e ./e2e/... -count=1
```

### 10. Validate the complete change

Run in this order and fix each failure before proceeding:

```sh
rtk gofmt -w internal/autosuggest/*.go internal/config/config.go internal/config/config_test.go internal/history/store.go internal/history/store_test.go internal/prediction/predictor.go internal/prediction/predictor_test.go internal/prediction/store.go internal/prediction/store_test.go internal/completion/types.go internal/completion/router.go internal/completion/router_test.go internal/shell/completion_feedback.go internal/shell/completion_feedback_test.go internal/editor/editor.go internal/editor/ghost.go internal/editor/ghost_test.go internal/editor/display.go internal/editor/display_test.go internal/editor/insert.go internal/editor/integration_test.go internal/shell/shell.go internal/shell/shell_test.go e2e/autosuggestion_terminal_test.go
rtk go test -race ./internal/autosuggest/... ./internal/editor/... ./internal/history/... ./internal/prediction/... ./internal/completion/... ./internal/config/... ./internal/shell/...
rtk go test ./...
rtk go test -tags=e2e ./e2e/...
rtk go vet ./...
rtk git diff --check
rtk git status --short
```

The `gofmt` command is a mechanical formatter exception to the source-editing rule; all intentional source edits must still be made with `apply_patch`.

## Test matrix

| Layer | Must prove |
|---|---|
| Config | defaults, order preservation, validation, TOML decoding |
| Engine | ordered fallback, strict prefix, cancellation, ignore/max/min rules |
| History | literal prefix, latest successful result, special characters, context cancellation |
| Previous command | correct context, prefix restriction, CWD ranking, fallback, no-history case |
| Completion | no agent calls, no fuzzy replacement, timeout, cancellation, Tab preemption |
| Editor | non-blocking input, newest result wins, local suffix reuse, lifecycle cancellation |
| Acceptance | full, partial, UTF-8, punctuation, Tab/Escape/Enter, agent precedence |
| Display | single-line, wrapping, multi-line, cursor position, resize |
| Shell | dependency wiring, previous-command metadata, prompt ghost isolation |
| PTY | actual key sequences and terminal output under rapid typing |

## Done criteria

The work is complete only when all of these are true:

- `[autosuggestions]` is documented and invalid configuration fails clearly.
- Default behavior remains the current local history suggestion with no new speculative completion work.
- Configured strategies execute in order and return only strict extensions.
- A slow provider never blocks key processing, and stale results cannot become visible.
- Every request is canceled or rendered; race tests report no leaks or data races.
- The completion strategy cannot invoke `PriorityAgent` or any future provider marked remote/expensive.
- Interactive Tab completion preempts speculative completion.
- Right and End accept all; partial acceptance is UTF-8 safe; Enter never executes ghost-only text.
- Agent, learned-fix, and empty-prompt prediction behavior remains covered and unchanged.
- Multi-line ghost text renders without corrupting wrapping or cursor placement.
- All commands in step 10 exit zero.
- `git diff --check` is clean and `git status --short` contains only intentional files.

## Stop conditions

Stop and ask for direction if any of these occurs:

- uncommitted overlapping editor/shell changes have no explicit owner or integration decision;
- implementing speculative completion would require calling `PriorityAgent`, a network service, or a paid provider;
- the router cannot let interactive completion preempt speculative work without redesigning shared in-flight state;
- a provider returns edits that cannot be represented as an exact typed-prefix extension;
- multi-line rendering requires replacing the display model rather than extending existing layout primitives;
- preserving current agent ghost ownership conflicts with the concurrent-streaming work found in the worktree;
- the same focused verification fails twice for reasons outside the changed code;
- schema migration becomes necessary for `command_sequences` rather than adding a compatible indexed query.

## Git workflow

After the drift check and only when the worktree is safe:

```sh
rtk git switch -c codex/001-hash-native-autosuggestions
```

Commit cohesive slices rather than one large commit. Suggested subjects:

```text
feat(config): add autosuggestion settings
feat(autosuggest): add ordered suggestion strategies
feat(completion): add safe speculative completion
feat(editor): make ghost suggestions asynchronous
test(e2e): cover autosuggestion terminal behavior
```

Do not stage unrelated dirty files. Before each commit, inspect `rtk git diff --cached --stat` and `rtk git diff --cached`.

## Maintenance notes

- Any new strategy must accept a context and must return an exact full-line candidate, never a suffix or edit script.
- Any new completion provider must opt into speculative use explicitly; agent, remote, and expensive providers remain excluded by default.
- Keep interactive completion latency more important than background suggestion freshness.
- Keep strategy orchestration free of editor state so it remains unit-testable.
- When changing editor actions, add autosuggestion invalidation/acceptance cases beside the action's existing tests.
- Recheck upstream only for behavioral ideas. Hash's public contract is its own config and editor model.
