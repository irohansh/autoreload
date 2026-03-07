package process

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
	pollInterval    = 100 * time.Millisecond
)

type Cmd struct {
	cmd    *exec.Cmd
	logger *slog.Logger
	mu     sync.Mutex
}

func Start(ctx context.Context, name string, args []string, workDir string, logger *slog.Logger) (*Cmd, error) {
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

	p := &Cmd{cmd: cmd, logger: logger}
	return p, nil
}

func StartWithShell(ctx context.Context, command string, workDir string, logger *slog.Logger) (*Cmd, error) {
	var shell string
	var flag string
	if runtime.GOOS == "windows" {
		shell = "cmd"
		flag = "/C"
	} else {
		shell = "sh"
		flag = "-c"
	}
	return Start(ctx, shell, []string{flag, command}, workDir, logger)
}

func (p *Cmd) Kill() error {
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
	deadline := time.Now().Add(gracefulTimeout)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); err == syscall.ESRCH {
			return nil
		}
		time.Sleep(pollInterval)
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		if err == syscall.ESRCH {
			return nil
		}
		return err
	}
	time.Sleep(killTimeout)
	return nil
}

func (p *Cmd) killWindows(pid int) error {
	cmd := exec.Command("taskkill", "/pid", fmt.Sprintf("%d", pid), "/t", "/f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil && p.cmd.Process != nil {
		p.cmd.Process.Kill()
	}
	return nil
}

func (p *Cmd) Wait() error {
	if p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}

func (p *Cmd) PID() int {
	if p.cmd == nil || p.cmd.Process == nil {
		return 0
	}
	return p.cmd.Process.Pid
}
