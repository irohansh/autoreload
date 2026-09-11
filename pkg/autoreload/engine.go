// Package autoreload watches a project directory and rebuilds/restarts a
// server process when relevant source files change. It powers the autoreload
// CLI and can also be embedded in other Go programs via the Engine API.
package autoreload

import (
	"context"
	"errors"
	"log/slog"
	"os"
)

// Options configures an Engine. Root, Build and Exec are required.
type Options struct {
	Root       string
	Build      string
	Exec       string
	Ignore     []string
	Extensions []string
	Logger     *slog.Logger
}

// Engine ties together the file watcher and the build/run loop.
type Engine struct {
	opts          Options
	logger        *slog.Logger
	w             *watcher
	r             *runner
	manualRestart chan struct{}
}

// New validates opts and constructs an Engine. The file watcher starts
// observing immediately; call Run to build and serve, and Close to release it.
func New(opts Options) (*Engine, error) {
	if opts.Root == "" || opts.Build == "" || opts.Exec == "" {
		return nil, errors.New("autoreload: Root, Build and Exec are required")
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}

	w, err := newWatcher(opts.Root, logger, opts.Ignore, opts.Extensions)
	if err != nil {
		return nil, err
	}

	return &Engine{
		opts:          opts,
		logger:        logger,
		w:             w,
		r:             newRunner(opts.Build, opts.Exec, opts.Root, logger),
		manualRestart: make(chan struct{}, 1),
	}, nil
}

// Run builds, starts the server, and rebuilds/restarts on file changes until
// the loop ends or ctx is cancelled. On cancellation it shuts down gracefully.
func (e *Engine) Run(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- e.r.Run(e.w.Changes(), e.manualRestart)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		e.logger.Info("[autoreload] shutting down...")
		e.w.Close()
		return <-done
	}
}

// Restart requests a manual rebuild and server restart. It is non-blocking and
// coalesces with any pending request.
func (e *Engine) Restart() {
	select {
	case e.manualRestart <- struct{}{}:
	default:
	}
}

// Close releases the file watcher. It is safe to call multiple times.
func (e *Engine) Close() error {
	return e.w.Close()
}
