package runner

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/rohan/hotreload/internal/process"
)

const (
	crashThreshold  = 3 * time.Second
	initialBackoff  = 1 * time.Second
	maxBackoff      = 16 * time.Second
)

type Runner struct {
	root        string
	buildCmd    string
	execCmd     string
	logger      *slog.Logger
	buildCancel context.CancelFunc
	buildMu     sync.Mutex
	server      *process.Cmd
	serverDone  chan struct{}
	serverMu    sync.Mutex
	serverStart time.Time
	backoff     time.Duration
}

func New(buildCmd, execCmd, root string, logger *slog.Logger) *Runner {
	absRoot, _ := filepath.Abs(root)
	return &Runner{
		root:     absRoot,
		buildCmd: buildCmd,
		execCmd:  execCmd,
		logger:   logger,
	}
}

func (r *Runner) Run(changes <-chan struct{}) error {
	if err := r.runBuild(context.Background()); err != nil {
		return err
	}
	r.startServer(context.Background())

	for {
		select {
		case _, ok := <-changes:
			if !ok {
				r.killServer()
				return nil
			}
			r.restart()
		case <-r.getServerDone():
			r.onServerExited(changes)
		}
	}
}

func (r *Runner) onServerExited(changes <-chan struct{}) {
	runtime := time.Since(r.serverStart)
	if runtime < crashThreshold {
		if r.backoff == 0 {
			r.backoff = initialBackoff
		}
		r.logger.Info("server crashed quickly, applying backoff before restart", "ran", runtime, "backoff", r.backoff)
		time.Sleep(r.backoff)
		if r.backoff < maxBackoff {
			r.backoff *= 2
		}
	} else {
		r.backoff = 0
		r.logger.Info("server exited, restarting")
	}
	r.restart()
}

func (r *Runner) restart() {
	r.killServer()

	r.buildMu.Lock()
	if r.buildCancel != nil {
		r.buildCancel()
		r.buildCancel = nil
	}
	r.buildMu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	r.buildMu.Lock()
	r.buildCancel = cancel
	r.buildMu.Unlock()

	go func() {
		if err := r.runBuild(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			r.logger.Error("build failed", "error", err)
			return
		}
		if ctx.Err() != nil {
			return
		}
		r.startServer(ctx)
	}()
}

func (r *Runner) runBuild(ctx context.Context) error {
	r.logger.Info("building...")
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", r.buildCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", r.buildCmd)
	}
	cmd.Dir = r.root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (r *Runner) startServer(ctx context.Context) {
	r.serverMu.Lock()
	defer r.serverMu.Unlock()

	server, err := process.StartWithShell(ctx, r.execCmd, r.root, r.logger)
	if err != nil {
		r.logger.Error("failed to start server", "error", err)
		return
	}

	serverDone := make(chan struct{})
	r.server = server
	r.serverDone = serverDone
	r.serverStart = time.Now()
	go func() {
		server.Wait()
		close(serverDone)
	}()
	r.logger.Info("server started", "pid", server.PID())
}

func (r *Runner) killServer() {
	r.serverMu.Lock()
	server := r.server
	serverDone := r.serverDone
	r.server = nil
	r.serverDone = nil
	r.serverMu.Unlock()

	if server != nil {
		server.Kill()
		if serverDone != nil {
			<-serverDone
		}
	}
}

func (r *Runner) getServerDone() <-chan struct{} {
	r.serverMu.Lock()
	defer r.serverMu.Unlock()
	return r.serverDone
}
