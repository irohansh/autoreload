package autoreload

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"
)

const (
	gracefulTimeout = 5 * time.Second
	killTimeout     = 2 * time.Second
)

type procCmd struct {
	cmd     *exec.Cmd
	logger  *slog.Logger
	mu      sync.Mutex
	done    chan struct{}
	waitErr error
}

func startProcess(ctx context.Context, name string, args []string, workDir string, logger *slog.Logger) (*procCmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	p := &procCmd{cmd: cmd, logger: logger, done: make(chan struct{})}
	// Single reaper: the only caller of cmd.Wait(), reaping the child on exit so Wait()/Kill() just observe done.
	go func() {
		p.waitErr = p.cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

func startWithShell(ctx context.Context, command string, workDir string, logger *slog.Logger) (*procCmd, error) {
	var shell string
	var flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/C"
	} else {
		shell = "sh"
		flag = "-c"
	}
	return startProcess(ctx, shell, []string{flag, command}, workDir, logger)
}

func (p *procCmd) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil || p.cmd.Process == nil {
		return nil
	}
	pid := p.cmd.Process.Pid
	if runtime.GOOS == "windows" {
		return p.killWindows(pid)
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		p.logger.Debug("SIGTERM failed, trying SIGKILL", "pid", pid, "error", err)
	}
	// Wait on the actual exit signalled by the reaper, not a poll loop.
	select {
	case <-p.done:
		return nil
	case <-time.After(gracefulTimeout):
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		// EPERM/ESRCH on the kill itself means it is already gone, not a failure.
		if err == syscall.ESRCH || err == syscall.EPERM {
			p.logger.Debug("SIGKILL: process already gone", "pid", pid, "error", err)
			return nil
		}
		return err
	}
	select {
	case <-p.done:
	case <-time.After(killTimeout):
	}
	return nil
}

func (p *procCmd) killWindows(pid int) error {
	cmd := exec.Command("taskkill", "/pid", fmt.Sprintf("%d", pid), "/t", "/f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	return nil
}

func (p *procCmd) Wait() error {
	if p.cmd == nil || p.done == nil {
		return nil
	}
	<-p.done
	return p.waitErr
}

func (p *procCmd) PID() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
