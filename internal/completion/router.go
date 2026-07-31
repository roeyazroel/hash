package completion

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tfcace/hash/internal/trace"
)

// Router dispatches completion requests to registered completers.
type Router struct {
	completers        []registeredCompleter
	nextCompleterID   uint64
	fuzzy             bool
	completerMu       sync.Mutex
	completerInFlight map[completerInFlightKey]completerInFlightCall
	prefetchMu        sync.Mutex
	prefetchInFlight  map[uint64]prefetchInFlightCall
}

type registeredCompleter struct {
	completer   Completer
	priority    Priority
	id          uint64
	speculative bool
}

type boundedCompletionResult struct {
	result Result
	err    error
}

type completerCallResult struct {
	result Result
	err    error
}

type completerInFlightCall struct {
	started time.Time
}

type completerInFlightKey struct {
	id          uint64
	speculative bool
}

type prefetchInFlightCall struct {
	started time.Time
}

// NewRouter creates a new completion router.
func NewRouter() *Router {
	return &Router{}
}

// SetFuzzy enables or disables fuzzy filtering of results.
func (r *Router) SetFuzzy(enabled bool) {
	r.fuzzy = enabled
}

// Fuzzy returns whether fuzzy filtering is enabled.
func (r *Router) Fuzzy() bool {
	return r.fuzzy
}

// Register adds a completer with the given priority.
// Lower priority values are tried first.
func (r *Router) Register(c Completer, priority Priority) {
	r.register(c, priority, false)
}

// RegisterSpeculative registers a known-local completer for explicit opt-in
// background suggestions. Remote or expensive providers must use Register.
func (r *Router) RegisterSpeculative(c Completer, priority Priority) {
	r.register(c, priority, true)
}

func (r *Router) register(c Completer, priority Priority, speculative bool) {
	r.nextCompleterID++
	r.completers = append(r.completers, registeredCompleter{
		completer:   c,
		priority:    priority,
		id:          r.nextCompleterID,
		speculative: speculative,
	})

	// Sort by priority (lower first)
	sort.Slice(r.completers, func(i, j int) bool {
		if r.completers[i].priority != r.completers[j].priority {
			return r.completers[i].priority < r.completers[j].priority
		}
		return r.completers[i].id < r.completers[j].id
	})
}

// Complete tries each completer in priority order until one returns results.
func (r *Router) Complete(ctx context.Context, line string, pos int) (Result, error) {
	return r.complete(ctx, line, pos, false)
}

// CompleteSpeculative runs only local completers in a separate in-flight lane.
// It does not fuzzy-filter results and returns at most one item, since a ghost
// suggestion can display only one exact extension at a time.
func (r *Router) CompleteSpeculative(ctx context.Context, line string, pos int) (Result, error) {
	return r.complete(ctx, line, pos, true)
}

func (r *Router) complete(ctx context.Context, line string, pos int, speculative bool) (Result, error) {
	start := time.Now()
	traceEnabled := trace.Enabled("completion")

	// Extract the query (word being completed) for fuzzy filtering
	query := extractCompletionQuery(line, pos)
	if traceEnabled {
		trace.Emit("completion", "router_start", trace.LevelDetailed, map[string]any{
			"line":        line,
			"pos":         pos,
			"query":       query,
			"fuzzy":       r.fuzzy && !speculative,
			"speculative": speculative,
			"completers":  len(r.completers),
		})
	}

	for _, rc := range r.completers {
		if speculative && (!rc.speculative || rc.priority >= PriorityAgent) {
			continue
		}
		if ctx.Err() != nil {
			if traceEnabled {
				trace.Emit("completion", "router_canceled", trace.LevelDetailed, map[string]any{
					"duration_ms": float64(time.Since(start).Microseconds()) / 1000.0,
				})
			}
			return Result{}, nil
		}

		completerStart := time.Now()
		if traceEnabled {
			trace.Emit("completion", "completer_start", trace.LevelDetailed, map[string]any{
				"name":     rc.completer.Name(),
				"priority": int(rc.priority),
			})
		}

		result, err := r.completeWithBoundary(ctx, rc, line, pos, speculative)
		if traceEnabled {
			errText := ""
			if err != nil {
				errText = err.Error()
			}
			trace.Emit("completion", "completer_done", trace.LevelDetailed, map[string]any{
				"name":        rc.completer.Name(),
				"priority":    int(rc.priority),
				"items":       len(result.Items),
				"error":       errText,
				"duration_ms": float64(time.Since(completerStart).Microseconds()) / 1000.0,
			})
		}
		if err != nil {
			continue // Try next completer on error
		}

		if len(result.Items) > 0 {
			result = r.finalizeResult(result, query, !speculative)
			if speculative {
				result = firstStrictSpeculativeResult(line, pos, result)
			}
			if len(result.Items) > 0 {
				if traceEnabled {
					trace.Emit("completion", "router_done", trace.LevelDetailed, map[string]any{
						"winner":      rc.completer.Name(),
						"items":       len(result.Items),
						"duration_ms": float64(time.Since(start).Microseconds()) / 1000.0,
					})
				}
				return result, nil
			}
		}
	}

	if traceEnabled {
		trace.Emit("completion", "router_done", trace.LevelDetailed, map[string]any{
			"winner":      "",
			"items":       0,
			"duration_ms": float64(time.Since(start).Microseconds()) / 1000.0,
		})
	}
	return Result{}, nil
}

func (r *Router) finalizeResult(result Result, query string, fuzzy bool) Result {
	if fuzzy && r.fuzzy && query != "" && !strings.HasSuffix(query, "/") {
		filterQuery := basenameCompletionQuery(query)
		if filterQuery != "" {
			result.Items = FuzzyFilter(result.Items, filterQuery)
		}
	}
	result.Items = limitCompletionItems(result.Items)
	return result
}

// firstStrictSpeculativeResult selects the first completion which can be
// rendered as a strict extension of the whole current line. Unlike
// interactive completion, the speculative lane does not fuzzy-filter results,
// so the raw first item is not necessarily suitable for a ghost suggestion.
func firstStrictSpeculativeResult(line string, pos int, result Result) Result {
	for _, item := range result.Items {
		candidate, _, ok := ReconstructLine(line, pos, result, item)
		if ok && candidate != line && strings.HasPrefix(candidate, line) {
			result.Items = []Item{item}
			return result
		}
	}
	result.Items = nil
	return result
}

func basenameCompletionQuery(query string) string {
	if lastSlash := strings.LastIndex(query, "/"); lastSlash >= 0 {
		return query[lastSlash+1:]
	}
	return query
}

func (r *Router) completeWithBoundary(ctx context.Context, rc registeredCompleter, line string, pos int, speculative bool) (Result, error) {
	key := completerInFlightKey{id: rc.id, speculative: speculative}
	if !r.beginCompleterCall(key) {
		return Result{}, nil
	}

	done := make(chan completerCallResult, 1)
	go func() {
		defer r.endCompleterCall(key)
		result, err := rc.completer.Complete(ctx, line, pos)
		done <- completerCallResult{result: result, err: err}
	}()

	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case result := <-done:
		return result.result, result.err
	}
}

func (r *Router) beginCompleterCall(key completerInFlightKey) bool {
	r.completerMu.Lock()
	defer r.completerMu.Unlock()
	if r.completerInFlight == nil {
		r.completerInFlight = make(map[completerInFlightKey]completerInFlightCall)
	}
	now := time.Now()
	if call, ok := r.completerInFlight[key]; ok {
		if !contextReadCallIsStale(call.started, now, 0) {
			return false
		}
	}
	r.completerInFlight[key] = completerInFlightCall{started: now}
	return true
}

func (r *Router) endCompleterCall(key completerInFlightKey) {
	r.completerMu.Lock()
	delete(r.completerInFlight, key)
	r.completerMu.Unlock()
}

// CompleteBounded runs completion behind the provided context boundary.
// This protects UI callers from completers that ignore context cancellation.
func (r *Router) CompleteBounded(ctx context.Context, line string, pos int) (Result, error) {
	return r.completeBounded(ctx, line, pos, r.Complete)
}

// CompleteSpeculativeBounded is CompleteSpeculative with the same worker
// boundary used by interactive completion for providers that ignore contexts.
func (r *Router) CompleteSpeculativeBounded(ctx context.Context, line string, pos int) (Result, error) {
	return r.completeBounded(ctx, line, pos, r.CompleteSpeculative)
}

func (r *Router) completeBounded(ctx context.Context, line string, pos int, complete func(context.Context, string, int) (Result, error)) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}

	done := make(chan boundedCompletionResult, 1)
	go func() {
		result, err := complete(ctx, line, pos)
		done <- boundedCompletionResult{result: result, err: err}
	}()

	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case result := <-done:
		return result.result, result.err
	}
}

// extractCompletionQuery extracts the word being completed.
func extractCompletionQuery(line string, pos int) string {
	return shellUnescapeWord(shellWordAt(line, pos))
}

// ExtractPipeContext extracts the command context after the last pipe.
// For "cat file | pb", returns ("pb", 3) where 3 is the new position.
// For "ls -la", returns ("ls -la", 5) unchanged.
// This allows completers to work correctly with piped commands.
func ExtractPipeContext(line string, pos int) (extracted string, newPos int) {
	pos = clampCursor(line, pos)

	// Find the last pipe character before pos
	lastPipe := -1
	for i := pos - 1; i >= 0; i-- {
		if line[i] == '|' {
			lastPipe = i
			break
		}
	}

	if lastPipe < 0 {
		// No pipe, return original
		return line, pos
	}

	// Skip the pipe and any leading whitespace
	start := lastPipe + 1
	for start < pos && (line[start] == ' ' || line[start] == '\t') {
		start++
	}

	// Return the segment after the pipe with adjusted position
	extracted = line[start:pos]
	newPos = pos - start
	return extracted, newPos
}

// Completers returns the registered completers for inspection.
func (r *Router) Completers() []Completer {
	result := make([]Completer, len(r.completers))
	for i, rc := range r.completers {
		result[i] = rc.completer
	}
	return result
}

// Prefetcher is an optional interface for completers that support background prefetching.
type Prefetcher interface {
	Prefetch(line string, pos int)
}

// Prefetch triggers background prefetching for all completers that support it.
// Call this when the user types a space after a command.
func (r *Router) Prefetch(line string, pos int) {
	for _, rc := range r.completers {
		if p, ok := rc.completer.(Prefetcher); ok {
			p.Prefetch(line, pos)
		}
	}
}

// PrefetchBounded runs prefetch work in the background and coalesces stuck prefetchers.
func (r *Router) PrefetchBounded(line string, pos int) {
	for _, rc := range r.completers {
		p, ok := rc.completer.(Prefetcher)
		if !ok {
			continue
		}
		if !r.beginPrefetch(rc.id) {
			continue
		}
		go func(id uint64, prefetcher Prefetcher) {
			defer r.endPrefetch(id)
			prefetcher.Prefetch(line, pos)
		}(rc.id, p)
	}
}

func (r *Router) beginPrefetch(key uint64) bool {
	r.prefetchMu.Lock()
	defer r.prefetchMu.Unlock()
	if r.prefetchInFlight == nil {
		r.prefetchInFlight = make(map[uint64]prefetchInFlightCall)
	}
	now := time.Now()
	if call, ok := r.prefetchInFlight[key]; ok {
		if !contextReadCallIsStale(call.started, now, 0) {
			return false
		}
	}
	r.prefetchInFlight[key] = prefetchInFlightCall{started: now}
	return true
}

func (r *Router) endPrefetch(key uint64) {
	r.prefetchMu.Lock()
	delete(r.prefetchInFlight, key)
	r.prefetchMu.Unlock()
}
