package stages

import (
	"context"
	"sync"
)

// parallel runs every fn concurrently and returns the FIRST non-nil error,
// cancelling the context handed to the other fns the instant any one fails.
//
// It is a deliberate, minimal, stdlib-only in-house equivalent of
// golang.org/x/sync/errgroup's errgroup.WithContext + g.Go + g.Wait —
// byte-for-byte SEMANTICALLY, not by source. See the package CLAUDE.md
// "errgroup SSOT reconciliation (offline-build constraint)" entry: the
// architecture (ai-agents-pipeline.md:38, :1681-1703) illustrates parallel
// stages with errgroup as the reference realization of a SEMANTIC contract
// (":1703" — "При ошибке в одной из goroutines — gctx отменяется, остальные
// goroutines прерывают LLM-вызовы"); golang.org/x/sync is absent from
// go.mod/go.sum and unobtainable in the build sandbox (no network, no module
// cache), and an unresolvable import would fail the task's own
// make build/test/lint gate. This helper satisfies the semantic contract
// with ZERO third-party code — keeping the Stage Executor fully hermetic and
// schemavalidator the codebase's single non-hermetic exception
// (code-architect re-adjudicated D1 → Option A, binding constraints B-1..B-5).
//
// Guarantees (the frozen behavioural contract):
//   - every fn receives a child of ctx (dctx), never ctx itself;
//   - the FIRST fn to return a non-nil error wins; that exact error value is
//     returned UNWRAPPED so errors.As(*model.DomainError) survives the join
//     (load-bearing — the Orchestrator maps *model.DomainError onto
//     lic.events.status-changed; code-architect D4 / additional-finding-3);
//   - on that first error dctx is cancelled so sibling agents' in-flight
//     router.Complete / agent.Run abort their LLM HTTP calls (the literal
//     :1703 requirement);
//   - all goroutines are joined before parallel returns (no leak);
//   - all fns succeed ⇒ nil; the parent ctx is never cancelled by parallel
//     (only the internal dctx is, via defer).
//
// fns must be safe to run concurrently (the Stage Executor guarantees this:
// each closure runs one agent and writes its OWN disjoint *model.PipelineState
// field — the pipeline_state.go invariant, -race-pinned).
func parallel(ctx context.Context, fns ...func(context.Context) error) error {
	dctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	wg.Add(len(fns))
	for _, fn := range fns {
		fn := fn
		go func() {
			defer wg.Done()
			if err := fn(dctx); err != nil {
				once.Do(func() {
					first = err
					cancel()
				})
			}
		}()
	}
	wg.Wait()
	return first
}
