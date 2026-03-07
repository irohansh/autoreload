package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
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
	root             string
	buildCmd         string
	execCmd          string
	logger           *slog.Logger
	buildCancel      context.CancelFunc
	buildMu          sync.Mutex
	server           *process.Cmd
	serverDone       chan struct{}
	serverMu         sync.Mutex
	serverStart      time.Time
	backoff          time.Duration
	lastHash         string
	restartMu        sync.Mutex
	restartScheduled bool
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

func (r *Runner) Run(changes <-chan string, manualRestart <-chan struct{}) error {
	if err := r.runBuild(context.Background()); err != nil {
		return err
	}
	r.startServer(context.Background())

	buildRequest := make(chan string, 1)
	go r.buildWorker(buildRequest)

	for {
		select {
		case path, ok := <-changes:
			if !ok {
				r.killServer()
				close(buildRequest)
				return nil
			}
			r.scheduleBuild(path, buildRequest)
		case <-manualRestart:
			r.logger.Info("[hotreload] manual restart requested")
			r.scheduleBuild("", buildRequest)
		case <-r.getServerDone():
			r.clearServerDone()
			r.onServerExited(changes)
			r.restartMu.Lock()
			if r.restartScheduled {
				r.restartMu.Unlock()
				continue
			}
			r.restartScheduled = true
			r.restartMu.Unlock()
			r.scheduleBuild("", buildRequest)
		}
	}
}

func (r *Runner) scheduleBuild(path string, buildRequest chan string) {
	r.buildMu.Lock()
	if r.buildCancel != nil {
		r.buildCancel()
		r.buildCancel = nil
	}
	r.buildMu.Unlock()

	select {
	case buildRequest <- path:
	default:
		<-buildRequest
		buildRequest <- path
	}
}

func (r *Runner) buildWorker(buildRequest chan string) {
	for path := range buildRequest {
		r.killServer()

		ctx, cancel := context.WithCancel(context.Background())
		r.buildMu.Lock()
		r.buildCancel = cancel
		r.buildMu.Unlock()

		if path != "" {
			r.logger.Info("[hotreload] change detected", "path", path)
		}
		start := time.Now()
		if err := r.runBuild(ctx); err != nil {
			r.setRestartScheduled(false)
			if ctx.Err() != nil {
				continue
			}
			continue
		}
		if ctx.Err() != nil {
			r.setRestartScheduled(false)
			continue
		}
		r.logger.Info("[hotreload] build completed", "duration", time.Since(start))
		if r.shouldSkipRestart() {
			r.logger.Info("[hotreload] binary unchanged, skipping server restart")
			r.setRestartScheduled(false)
			continue
		}
		r.startServer(ctx)
		r.logger.Info("[hotreload] server restarted")
		r.setRestartScheduled(false)
	}
}

func (r *Runner) onServerExited(changes <-chan string) {
	runDuration := time.Since(r.serverStart)
	if runDuration < crashThreshold {
		if r.backoff == 0 {
			r.backoff = initialBackoff
		}
		r.logger.Info("[server] crashed quickly, applying backoff", "ran", runDuration, "backoff", r.backoff)
		time.Sleep(r.backoff)
		if r.backoff < maxBackoff {
			r.backoff *= 2
		}
	} else {
		r.backoff = 0
		r.logger.Info("[server] exited, restarting")
	}
}

func (r *Runner) runBuild(ctx context.Context) error {
	r.logger.Info("[build] starting...")
	start := time.Now()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", r.buildCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", r.buildCmd)
	}
	cmd.Dir = r.root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		r.logger.Error("[build] failed", "duration", time.Since(start), "error", err)
		return err
	}
	r.logger.Info("[build] ok", "duration", time.Since(start))
	return nil
}

func (r *Runner) startServer(ctx context.Context) {
	r.serverMu.Lock()
	defer r.serverMu.Unlock()

	server, err := process.StartWithShell(ctx, r.execCmd, r.root, r.logger)
	if err != nil {
		r.logger.Error("[server] failed to start", "error", err)
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
	r.logger.Info("[server] started", "pid", server.PID())
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

func (r *Runner) clearServerDone() {
	r.serverMu.Lock()
	defer r.serverMu.Unlock()
	r.server = nil
	r.serverDone = nil
}

func (r *Runner) setRestartScheduled(v bool) {
	r.restartMu.Lock()
	defer r.restartMu.Unlock()
	r.restartScheduled = v
}

func (r *Runner) execBinaryPath() string {
	fields := strings.Fields(r.execCmd)
	if len(fields) == 0 {
		return ""
	}
	bin := fields[0]
	if filepath.IsAbs(bin) {
		return bin
	}
	return filepath.Join(r.root, bin)
}

func (r *Runner) hashBinary(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (r *Runner) shouldSkipRestart() bool {
	path := r.execBinaryPath()
	if path == "" {
		return false
	}
	if info, err := os.Stat(path); err != nil || info == nil || !info.Mode().IsRegular() {
		return false
	}
	current := r.hashBinary(path)
	if current == "" {
		return false
	}
	if r.lastHash != "" && r.lastHash == current {
		return true
	}
	r.lastHash = current
	return false
}
