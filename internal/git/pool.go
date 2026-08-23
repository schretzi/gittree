package git

import (
	"context"
	"runtime"

	"golang.org/x/sync/errgroup"
)

// DefaultScanConcurrency is how many repositories are scanned at once when
// nothing else is specified. Scanning is CPU- and disk-bound rather than
// network-bound, so it scales with the machine.
func DefaultScanConcurrency() int { return max(runtime.NumCPU(), 2) }

// pool runs fn over items with at most n running at once.
//
// It deliberately uses a plain errgroup.Group rather than errgroup.WithContext:
// with the latter, the first repository that fails would cancel every other
// repository's work, which is precisely wrong here. One unreachable remote or
// one corrupt repository must not blank out the other ninety-nine. Per-item
// failures ride along in the result value instead, and cancellation comes only
// from the caller's context.
//
// The ctx.Err() check before each launch is what makes quitting prompt: without
// it, Wait still has to drain every queued closure before returning.
func pool[T any](ctx context.Context, items []T, n int, fn func(context.Context, T)) {
	if n < 1 {
		n = 1
	}
	var g errgroup.Group
	g.SetLimit(n)
	for _, item := range items {
		if ctx.Err() != nil {
			break
		}
		g.Go(func() error {
			fn(ctx, item)
			return nil
		})
	}
	_ = g.Wait() // fn never returns an error; failures live on the result
}

// ScanAll scans every directory concurrently and returns the results in the
// same order as dirs.
//
// A repository that fails to scan comes back with Repo.Err set rather than
// being dropped, so the caller can show it as broken instead of silently
// losing it.
func (c Client) ScanAll(ctx context.Context, dirs []string, concurrency int) []*Repo {
	out := make([]*Repo, len(dirs))
	type job struct {
		i   int
		dir string
	}
	jobs := make([]job, len(dirs))
	for i, d := range dirs {
		jobs[i] = job{i, d}
	}
	pool(ctx, jobs, concurrency, func(ctx context.Context, j job) {
		out[j.i] = c.Scan(ctx, j.dir)
	})
	// A cancelled run leaves holes; fill them so callers never see a nil.
	for i, r := range out {
		if r == nil {
			out[i] = &Repo{Dir: dirs[i], Err: ctx.Err()}
		}
	}
	return out
}
